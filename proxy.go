//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
)

/*
 Proxy is the primary interface to connect router to its peer router.
 At both ends of a connection, there is a proxy object for its router.
 Simple router connection can be set up thru calling Router.Connect().
 Proxy.Connect() can be used to set up more complicated connections,
 such as setting IdFilter to allow only a subset of messages pass thru
 the connection, or setting IdTranslator which can "relocate" remote message
 ids into a subspace in local router's namespace, or setting a flow control policy.
 Proxy.Close() is called to disconnect router from its peer.
*/
type Proxy interface {
	//Connect to a local router
	Connect(Proxy) error
	//Connect to a remote router thru io conn
	//1. io.ReadWriteCloser: transport connection
	//2. MarshalingPolicy: gob or json marshaling
	//3. remaining args can be a FlowControlPolicy (e.g. window based or XOnOff)
	ConnectRemote(io.ReadWriteCloser, MarshalingPolicy, ...interface{}) error
	//close proxy and disconnect from peer
	Close()
	//query messaging interface with peer
	LocalPubInfo() []*ChanInfo
	LocalSubInfo() []*ChanInfo
	PeerPubInfo() []*ChanInfo
	PeerSubInfo() []*ChanInfo
}

/*
 Peers are to be connected thru forwarding channels
 Proxy and Stream are peers
*/
type peerIntf interface {
	//find chan for forwarding app msgs (with id) to peer
	appMsgChanForId(Id) (Channel, int)
	//send ctrl msgs (Pub/UnPub/...) to peer
	sendCtrlMsg(*genericMsg) error
}

type proxyImpl struct {
	//chan for ctrl msgs from peers during connSetup
	ctrlChan chan *genericMsg
	//use peerIntf to forward app/ctrl msgs to peer (proxy or stream)
	peer peerIntf
	//my/local router
	router *routerImpl
	//chans connecting to local router on behalf of peer
	sysChans     *sysChanSet
	appSendChans *sendChanSet //send to local router
	appRecvChans *recvChanSet //recv from local router
	//filter & translator & flowController
	filter         IdFilter
	translator     IdTranslator
	flowController FlowControlPolicy
	//cache of export/import ids at proxy
	exportSendIds map[interface{}]*ChanInfo //exported send ids, global publish
	exportRecvIds map[interface{}]*ChanInfo //exported recv ids, global subscribe
	importSendIds map[interface{}]*ChanInfo //imported send ids from peer
	importRecvIds map[interface{}]*ChanInfo //imported recv ids from peer
	//mutex to protect cache
	inwardLock  sync.Mutex //protect exportRecvIds, importSendIds
	outwardLock sync.Mutex //protect exportSendIds, importRecvIds
	proxyLock   sync.Mutex //protect other proxy state
	//for log/debug
	name string
	Logger
	FaultRaiser
	//others
	connReady bool
	Closed    bool
	errChan   chan error
}

/*
 Proxy constructor. It accepts the following arguments:
    1. r:    the router which will be bound with this proxy and be owner of this proxy
    2. name: proxy's name, used in log messages if owner router's log is turned on
    3. f:    IdFilter to be installed at this proxy
    4. t:    IdTranslator to be installed at this proxy
*/
func NewProxy(r Router, name string, f IdFilter, t IdTranslator) Proxy {
	p := new(proxyImpl)
	p.router = r.(*routerImpl)
	p.name = name
	p.filter = f
	p.translator = t
	//create chan for incoming ctrl msgs during connSetup
	p.ctrlChan = make(chan *genericMsg, DefCmdChanBufSize)
	//chans to local router
	p.sysChans = newSysChanSet(p)
	p.appSendChans = &sendChanSet{newChanSet(p, ScopeLocal, MemberRemote)}
	p.appRecvChans = &recvChanSet{newChanSet(p, ScopeLocal, MemberRemote)}
	//cache: only need to create import cache, since export cache are queried/returned from router
	p.importSendIds = make(map[interface{}]*ChanInfo)
	p.importRecvIds = make(map[interface{}]*ChanInfo)
	p.router.addProxy(p)
	ln := ""
	if len(p.router.name) > 0 {
		if len(p.name) > 0 {
			ln = p.router.name + p.name
		} else {
			ln = p.router.name + "_proxy"
		}
	}
	p.Logger.Init(p.router.SysID(RouterLogId), p.router, ln)
	p.FaultRaiser.Init(p.router.SysID(RouterFaultId), p.router, ln)
	return p
}

func (p1 *proxyImpl) Connect(pp Proxy) error {
	p2 := pp.(*proxyImpl)
	p2.peer = p1
	p1.peer = p2
	p1.errChan = make(chan error)
	p1.start()
	p2.start()
	return <-p1.errChan
}

func (p *proxyImpl) ConnectRemote(rwc io.ReadWriteCloser, mar MarshalingPolicy, args ...interface{}) error {
	if len(args) > 0 {
		var ok bool
		p.flowController, ok = args[0].(FlowControlPolicy)
		if !ok {
			return errors.New("Proxy ConnectRemote(): invalid argument for FlowControlPolicy")
		}
	}
	s := newStream(rwc, mar, p)
	s.peer = p
	p.peer = s
	p.errChan = make(chan error)
	p.start()
	s.start()
	return <-p.errChan
}

func (p *proxyImpl) start() { go p.connInit() }

func (p *proxyImpl) Close() {
	p.Log(LOG_INFO, "proxy.Close() is called")
	p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(DisconnId), &ConnInfoMsg{}})
	p.closeImpl()
}

func (p *proxyImpl) closeImpl() {
	p.proxyLock.Lock()
	closed := p.Closed
	if !p.Closed {
		p.Closed = true
	}
	p.proxyLock.Unlock()
	if !closed {
		pubInfo := p.PeerPubInfo()
		subInfo := p.PeerSubInfo()
		//notify local chan that remote peer is leaving (unpub, unsub)
		p.Log(LOG_INFO, fmt.Sprintf("unpub [%d], unsub [%d]", len(pubInfo), len(subInfo)))
		if len(pubInfo) > 0 {
			p.Log(LOG_INFO, fmt.Sprintf("unpub info 1 [%v]", pubInfo))
		}
		if len(subInfo) > 0 {
			p.Log(LOG_INFO, fmt.Sprintf("unsub info 1 [%v]", subInfo))
		}
		p.sysChans.SendSysMsg(UnSubId, &ChanInfoMsg{subInfo})
		p.sysChans.SendSysMsg(UnPubId, &ChanInfoMsg{pubInfo})
		//start closing
		p.Log(LOG_INFO, "proxy closing")
		p.router.delProxy(p)
		//close my chans
		p.outwardLock.Lock()
		p.appRecvChans.Close()
		p.outwardLock.Unlock()
		p.inwardLock.Lock()
		p.appSendChans.Close()
		p.inwardLock.Unlock()
		p.Log(LOG_INFO, "proxy closed")
		//close logger
		p.FaultRaiser.Close()
		p.Logger.Close()
		//last
		p.sysChans.Close()
	}
}

const (
	raw            = iota //no flow control
	async                 //unlimited internal buffering
	flowControlled        //flow controlled
)

func (p *proxyImpl) connType() string {
	switch {
	case p.router.async:
		return "async"
	case p.flowController != nil:
		return p.flowController.String()
	}
	return "raw"
}

func (p *proxyImpl) connSetup() error {
	r := p.router
	//1. to initiate conn setup handshaking, send my conn info to peer
	p.peer.sendCtrlMsg(&genericMsg{r.SysID(ConnId), &ConnInfoMsg{Id: r.seedId, Type: p.connType()}})
	//2. recv connInfo from peer
	switch m := <-p.ctrlChan; m.Id.SysIdIndex() {
	case ConnId:
		//save peer conninfo & forward it to local subscribers
		ci := m.Data.(*ConnInfoMsg)
		p.sysChans.SendSysMsg(ConnId, ci)
		//check type info
		if reflect.TypeOf(ci.Id) != reflect.TypeOf(r.seedId) || ci.Type != p.connType() {
			err := errors.New(errRouterTypeMismatch)
			ci.Error = err.Error()
			//tell local listeners about the fail
			p.sysChans.SendSysMsg(ErrorId, ci)
			//tell peer about fail
			p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), &ConnInfoMsg{Error: err.Error()}})
			p.LogError(err)
			return err
		}
	default:
		err := errors.New(errConnInvalidMsg)
		//tell peer about fail
		errMsg := &ConnInfoMsg{Error: err.Error()}
		p.sysChans.SendSysMsg(ErrorId, errMsg)
		p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), errMsg})
		p.LogError(err)
		return err
	}
	//3. send initial pub/sub info to peer
	p.peer.sendCtrlMsg(&genericMsg{r.SysID(PubId), p.initPubInfoMsg()})
	p.peer.sendCtrlMsg(&genericMsg{r.SysID(SubId), p.initSubInfoMsg()})
	//4. handle init_pub/sub msgs, send connReady to peer and wait for peer's connReady
	peerReady := false
	myReady := false
	for !(peerReady && myReady) {
		switch m := <-p.ctrlChan; m.Id.SysIdIndex() {
		case ErrorId:
			ci := m.Data.(*ConnInfoMsg)
			p.sysChans.SendSysMsg(ErrorId, ci)
			ee := errors.New(ci.Error)
			p.LogError(ee)
			return ee
		case ReadyId:
			crm := m.Data.(*ConnReadyMsg)
			p.Log(LOG_INFO, fmt.Sprintf("recv readyId: %v", crm))
			if crm.Info != nil {
				_, err := p.handlePeerReadyMsg(m)
				p.sysChans.SendSysMsg(ReadyId, m.Data)
				if err != nil {
					//tell peer about fail
					errMsg := &ConnInfoMsg{Error: err.Error()}
					p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), errMsg})
					p.sysChans.SendSysMsg(ErrorId, errMsg)
					p.LogError(err)
					return err
				}
			} else {
				peerReady = true
			}
		case PubId:
			p.Log(LOG_INFO, "recv PubId")
			_, err := p.handlePeerPubMsg(m)
			p.sysChans.SendSysMsg(PubId, m.Data)
			if err != nil {
				//tell peer about fail
				errMsg := &ConnInfoMsg{Error: err.Error()}
				p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), errMsg})
				p.sysChans.SendSysMsg(ErrorId, errMsg)
				p.LogError(err)
				return err
			}
		case SubId:
			p.Log(LOG_INFO, "recv SubId")
			_, err := p.handlePeerSubMsg(m)
			p.sysChans.SendSysMsg(SubId, m.Data)
			if err != nil {
				//tell peer about fail
				errMsg := &ConnInfoMsg{Error: err.Error()}
				p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), errMsg})
				p.sysChans.SendSysMsg(ErrorId, errMsg)
				p.LogError(err)
				return err
			}
			myReady = true
			//tell peer i am ready by sending an empty ConnReadyMsg
			p.peer.sendCtrlMsg(&genericMsg{r.SysID(ReadyId), &ConnReadyMsg{}})
		default:
			err := errors.New(errConnInvalidMsg)
			p.Log(LOG_INFO, "recv invalid")
			//tell peer about fail
			errMsg := &ConnInfoMsg{Error: err.Error()}
			p.peer.sendCtrlMsg(&genericMsg{r.SysID(ErrorId), errMsg})
			p.sysChans.SendSysMsg(ErrorId, errMsg)
			p.LogError(err)
			return err
		}
	}
	return nil
}

func (p *proxyImpl) connInit() {
	//query router main goroutine to retrieve exported ids
	p.exportSendIds = p.router.IdsForSend(ExportedId)
	p.exportRecvIds = p.router.IdsForRecv(ExportedId)
	//filter out blocked ids
	if p.filter != nil {
		for k, v := range p.exportSendIds {
			if p.filter.BlockOutward(v.Id) {
				delete(p.exportSendIds, k)
			}
		}
		for k, v := range p.exportRecvIds {
			if p.filter.BlockInward(v.Id) {
				delete(p.exportRecvIds, k)
			}
		}
	}

	//start conn handshaking
	err := p.connSetup()
	if p.errChan != nil {
		p.errChan <- err
	}
	if err != nil {
		p.Log(LOG_INFO, "-- connection error")
		p.closeImpl()
		return
	}

	p.proxyLock.Lock()
	p.connReady = true
	p.proxyLock.Unlock()

	//drain the remaing peer ctrl msgs
	close(p.ctrlChan)
	for m := range p.ctrlChan {
		p.handlePeerCtrlMsg(m)
	}

	//start handling local ctrl msgs
	p.sysChans.StartHandleLocalCtrlMsg()

	p.Log(LOG_INFO, "-- connection ready")
	//proxy init goroutine exits here
}

func (p *proxyImpl) handleLocalCtrlMsg(m *genericMsg) {
	p.Log(LOG_INFO, "enter handleLocalCtrlMsg")
	var err error
	switch m.Id.SysIdIndex() {
	case PubId:
		_, err = p.handleLocalPubMsg(m)
	case UnPubId:
		_, err = p.handleLocalUnPubMsg(m)
	case SubId:
		_, err = p.handleLocalSubMsg(m)
	case UnSubId:
		_, err = p.handleLocalUnSubMsg(m)
	}
	if err != nil {
		//tell peer about fail
		ci := &ConnInfoMsg{Error: err.Error()}
		p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(ErrorId), ci})
		p.sysChans.SendSysMsg(ErrorId, ci)
		p.LogError(err)
	}
	p.Log(LOG_INFO, "exit handleLocalCtrlMsg")
	return
}

func (p *proxyImpl) handlePeerCtrlMsg(m *genericMsg) (err error) {
	switch m.Id.SysIdIndex() {
	case DisconnId:
		p.sysChans.SendSysMsg(DisconnId, m.Data)
		p.Log(LOG_INFO, "recv Disconn msg, proxy shutdown")
		p.closeImpl()
		return
	case ErrorId:
		ci := m.Data.(*ConnInfoMsg)
		p.sysChans.SendSysMsg(ErrorId, ci)
		p.LogError(errors.New(ci.Error))
		p.closeImpl()
		return
	case ReadyId:
		_, err = p.handlePeerReadyMsg(m)
		p.sysChans.SendSysMsg(ReadyId, m.Data)
	case PubId:
		_, err = p.handlePeerPubMsg(m)
		p.sysChans.SendSysMsg(PubId, m.Data)
	case UnPubId:
		_, err = p.handlePeerUnPubMsg(m)
		p.sysChans.SendSysMsg(UnPubId, m.Data)
	case SubId:
		_, err = p.handlePeerSubMsg(m)
		p.sysChans.SendSysMsg(SubId, m.Data)
	case UnSubId:
		_, err = p.handlePeerUnSubMsg(m)
		p.sysChans.SendSysMsg(UnSubId, m.Data)
	}
	if err != nil {
		//tell peer about fail
		ci := &ConnInfoMsg{Error: err.Error()}
		p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(ErrorId), ci})
		p.sysChans.SendSysMsg(ErrorId, ci)
		p.LogError(err)
		p.closeImpl()
	}
	return
}

//the following 2 functions are external interface exposed to peers
func (p *proxyImpl) sendCtrlMsg(m *genericMsg) (err error) {
	p.proxyLock.Lock()
	if !p.connReady {
		p.ctrlChan <- m
		p.proxyLock.Unlock()
	} else {
		closed := p.Closed
		p.proxyLock.Unlock()
		if !closed {
			err = p.handlePeerCtrlMsg(m)
		}
	}
	return
}

func (p *proxyImpl) appMsgChanForId(id Id) (Channel, int) {
	id1 := id
	if p.translator != nil {
		id1 = p.translator.TranslateInward(id)
	}
	p.inwardLock.Lock() //protect appSendChans
	defer p.inwardLock.Unlock()
	return p.appSendChans.findChan(id1)
}

func (p *proxyImpl) chanTypeMatch(info1, info2 *ChanInfo) bool {
	if info1.ChanType != nil {
		if info2.ChanType != nil {
			return info1.ChanType == info2.ChanType
		}
		//at here, info2 should be marshaled data from remote
		if info2.ElemType == nil {
			p.Log(LOG_ERROR, fmt.Sprintf("IdChanInfo miss both ChanType & ElemType info for %v", info2.Id))
			return false
		}
		//1. marshal data for info1
		if info1.ElemType == nil {
			info1.ElemType = new(chanElemTypeData)
		}
		if len(info1.ElemType.FullName) == 0 {
			info1.ElemType.FullName = getMsgTypeEncoding(info1.ChanType.Elem())
			//do the real type encoding
		}
		//2. compare marshaled data
		if info1.ElemType.FullName == info2.ElemType.FullName {
			//3. since type match, use info1's ChanType for info2
			info2.ChanType = info1.ChanType
			return true
		} else {
			p.Log(LOG_ERROR, fmt.Sprintf("ElemType.FullName mismatch1 %v, %v", info1.ElemType.FullName, info2.ElemType.FullName))
			return false
		}
	} else {
		if info2.ChanType == nil {
			p.Log(LOG_ERROR, fmt.Sprintf("both pub/sub miss ChanType for %v", info2.Id))
			return false
		}
		//at here, info1 should be marshaled data from remote
		if info1.ElemType == nil {
			p.Log(LOG_ERROR, fmt.Sprintf("IdChanInfo miss both ChanType & ElemType info for %v", info1.Id))
			return false
		}
		//1. marshal data for info2
		if info2.ElemType == nil {
			info2.ElemType = new(chanElemTypeData)
		}
		if len(info2.ElemType.FullName) == 0 {
			info2.ElemType.FullName = getMsgTypeEncoding(info2.ChanType.Elem())
			//do the real type encoding
		}
		//2. compare marshaled data
		if info1.ElemType.FullName == info2.ElemType.FullName {
			//3. since type match, use info1's ChanType for info2
			info1.ChanType = info2.ChanType
			return true
		} else {
			p.Log(LOG_ERROR, fmt.Sprintf("ElemType.FullName mismatch2 %v, %v", info1.ElemType.FullName, info2.ElemType.FullName))
			return false
		}
	}
	p.Log(LOG_ERROR, "chanTypeMatch failed at last")
	return false
}

//for the following 8 Local/Peer Pub/Sub mehtods, the general rule is that we set up
//msg sinks(senders to recv routers) before we set up msg sources(recvers from send routers) 
//so msg will not be lost
func (p *proxyImpl) handleLocalSubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	sInfo := msg.Info
	if len(sInfo) == 0 {
		return
	}
	numReady := 0
	readyInfo := make([]*ChanReadyInfo, len(sInfo))
	sInfo2 := make([]*ChanInfo, len(sInfo))
	p.inwardLock.Lock()
	for _, sub := range sInfo {
		p.Log(LOG_INFO, fmt.Sprintf("handleLocalSubMsg: %v", sub.Id))
		if p.filter != nil && p.filter.BlockInward(sub.Id) {
			continue
		}
		_, ok := p.exportRecvIds[sub.Id.Key()]
		if ok {
			continue
		}
		//here we have a new global sub id
		//update exported id cache
		p.exportRecvIds[sub.Id.Key()] = sub
		sInfo2[num] = sub
		num++ //one more to be exported
		//check if peer already pubed it
		for _, pub := range p.importSendIds {
			if sub.Id.Match(pub.Id) {
				if p.chanTypeMatch(sub, pub) {
					readyInfo[numReady] = &ChanReadyInfo{pub.Id, p.router.recvChanBufSize(sub.Id)}
					numReady++
					p.Log(LOG_INFO, fmt.Sprintf("send ConnReady for: %v", pub.Id))
					p.appSendChans.AddSender(pub.Id, pub.ChanType)
				} else {
					err = errors.New(errRmtChanTypeMismatch)
					p.LogError(err)
					p.inwardLock.Unlock()
					return
				}
			}
		}
	}
	p.inwardLock.Unlock()
	//send subscriptions to peer
	if num > 0 {
		sInfo2 = sInfo2[0:num]
		if p.translator == nil {
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{Info: sInfo2}})
		} else {
			for i, sub := range sInfo2 {
				sInfo2[i] = new(ChanInfo)
				sInfo2[i].Id = p.translator.TranslateOutward(sub.Id)
				sInfo2[i].ChanType = sub.ChanType
				sInfo2[i].ElemType = sub.ElemType
			}
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{Info: sInfo2}})
		}
	}
	//send connReadyInfo to peer
	if numReady > 0 {
		readyInfo = readyInfo[0:numReady]
		if p.translator == nil {
			p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(ReadyId), &ConnReadyMsg{Info: readyInfo}})
		} else {
			for i, ready := range readyInfo {
				readyInfo[i].Id = p.translator.TranslateOutward(ready.Id)
			}
			p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(ReadyId), &ConnReadyMsg{Info: readyInfo}})
		}
	}
	return
}

func (p *proxyImpl) handleLocalUnSubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	sInfo := msg.Info
	if len(sInfo) == 0 {
		return
	}
	sInfo2 := make([]*ChanInfo, len(sInfo))
	p.inwardLock.Lock()
	for _, sub := range sInfo {
		p.Log(LOG_INFO, fmt.Sprintf("handleLocalUnSubMsg: %v", sub.Id))
		_, ok := p.exportRecvIds[sub.Id.Key()]
		if !ok {
			continue
		}
		if p.filter != nil && p.filter.BlockInward(sub.Id) {
			continue
		}

		delCache := false
		delSender := false
		switch no := p.appSendChans.BindingCount(sub.Id); {
		case no < 0: //not in
			delCache = true
		case no == 0:
			delCache = true
			delSender = true
		case no > 0:
			continue
		}

		//update exported id cache
		if delCache {
			delete(p.exportRecvIds, sub.Id.Key())
			sInfo2[num] = sub
			num++
		}

		//check if peer already pubed it
		if delSender {
			pub, ok := p.importSendIds[sub.Id.Key()]
			if ok {
				p.appSendChans.DelChan(pub.Id)
			}
		}
	}
	p.inwardLock.Unlock()
	if num > 0 {
		sInfo2 = sInfo2[0:num]
		if p.translator == nil {
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{Info: sInfo2}})
		} else {
			for i, sub := range sInfo2 {
				sInfo2[i] = new(ChanInfo)
				sInfo2[i].Id = p.translator.TranslateOutward(sub.Id)
				sInfo2[i].ChanType = sub.ChanType
				sInfo2[i].ElemType = sub.ElemType
			}
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{Info: sInfo2}})
		}
	}
	return
}

func (p *proxyImpl) handleLocalPubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	pInfo := msg.Info
	if len(pInfo) == 0 {
		return
	}
	pInfo2 := make([]*ChanInfo, len(pInfo))
	p.outwardLock.Lock()
	for _, pub := range pInfo {
		p.Log(LOG_INFO, fmt.Sprintf("handleLocalPubMsg: %v", pub.Id))
		if p.filter != nil && p.filter.BlockOutward(pub.Id) {
			continue
		}
		_, ok := p.exportSendIds[pub.Id.Key()]
		if ok {
			continue
		}
		//update exported id cache
		p.exportSendIds[pub.Id.Key()] = pub
		pInfo2[num] = pub
		num++ //one more export
		//check if peer already subed it
		for _, sub := range p.importRecvIds {
			if pub.Id.Match(sub.Id) {
				if !p.chanTypeMatch(sub, pub) {
					err = errors.New(errRmtChanTypeMismatch)
					p.LogError(err)
					p.outwardLock.Unlock()
					return
				}
			}
		}
	}
	p.outwardLock.Unlock()
	if num > 0 {
		pInfo2 = pInfo2[0:num]
		if p.translator == nil {
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{pInfo2}})
		} else {
			for cnt, pub := range pInfo2 {
				pInfo2[cnt] = new(ChanInfo)
				pInfo2[cnt].Id = p.translator.TranslateOutward(pub.Id)
				pInfo2[cnt].ChanType = pub.ChanType
				pInfo2[cnt].ElemType = pub.ElemType
			}
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{pInfo2}})
		}
	}
	return
}

func (p *proxyImpl) handleLocalUnPubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	pInfo := msg.Info
	if len(pInfo) == 0 {
		return
	}
	pInfo2 := make([]*ChanInfo, len(pInfo))
	p.outwardLock.Lock()
	for _, pub := range pInfo {
		p.Log(LOG_INFO, fmt.Sprintf("handleLocalUnPubMsg: %v", pub.Id))
		_, ok := p.exportSendIds[pub.Id.Key()]
		if !ok {
			continue
		}
		if p.filter != nil && p.filter.BlockOutward(pub.Id) {
			continue
		}

		delCache := false
		delRecver := false
		switch no := p.appRecvChans.BindingCount(pub.Id); {
		case no < 0: //not in
			delCache = true
		case no == 0:
			delCache = true
			delRecver = true
		case no > 0:
			continue
		}

		//update exported id cache
		if delCache {
			delete(p.exportSendIds, pub.Id.Key())
			pInfo2[num] = pub
			num++
		}

		//check if peer already pubed it
		if delRecver {
			sub, ok := p.importRecvIds[pub.Id.Key()]
			if ok {
				p.appRecvChans.DelChan(sub.Id)
			}
		}
	}
	p.outwardLock.Unlock()
	if num > 0 {
		pInfo2 = pInfo2[0:num]
		if p.translator == nil {
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{pInfo2}})
		} else {
			for cnt, pub := range pInfo2 {
				pInfo2[cnt] = new(ChanInfo)
				pInfo2[cnt].Id = p.translator.TranslateOutward(pub.Id)
				pInfo2[cnt].ChanType = pub.ChanType
				pInfo2[cnt].ElemType = pub.ElemType
			}
			p.peer.sendCtrlMsg(&genericMsg{m.Id, &ChanInfoMsg{pInfo2}})
		}
	}
	return
}

func (p *proxyImpl) handlePeerReadyMsg(m *genericMsg) (num int, err error) {
	//p.Log(LOG_INFO, "handlePeerReadyMsg")
	msg := m.Data.(*ConnReadyMsg)
	rInfo := msg.Info
	if len(rInfo) == 0 {
		return
	}
	for _, ready := range rInfo {
		//update id scope/member
		ready.Id, _ = ready.Id.Clone(ScopeLocal, MemberRemote)
		if p.translator != nil {
			ready.Id = p.translator.TranslateInward(ready.Id)
		}
		//p.Log(LOG_INFO, fmt.Sprintf("handlePeerReadyMsg: %v, %v", ready.Id, ready.Credit))
		if p.filter != nil && p.filter.BlockOutward(ready.Id) {
			continue
		}
		//check
		p.outwardLock.Lock()
		r, _ := p.appRecvChans.findChan(ready.Id)
		if r != nil {
			p.outwardLock.Unlock()
			//p.Log(LOG_INFO, fmt.Sprintf("handlePeerReadyMsg22: %v, %v", ready.Id, ready.Credit))
			fs, ok := r.(FlowSender)
			if ok {
				fs.Ack(ready.Credit)
				//p.Log(LOG_INFO, fmt.Sprintf("handlePeerReadyMsg: add credit %v for recver %v", ready.Credit, ready.Id))
			} else {
				//p.Log(LOG_INFO, fmt.Sprintf("handlePeerReadyMsg: recver %v is not flow sender", ready.Id))
			}
		} else {
			//check if local already pubed it
			for _, pub := range p.exportSendIds {
				if pub.Id.Match(ready.Id) {
					//ready.Id, _ = ready.Id.Clone(ScopeLocal, MemberRemote)
					peerChan, _ := p.peer.appMsgChanForId(ready.Id)
					if peerChan != nil {
						p.appRecvChans.AddRecver(ready.Id, peerChan, ready.Credit)
						//p.Log(LOG_INFO, fmt.Sprintf("handlePeerReadyMsg, add recver for: %v, %v", ready.Id, ready.Credit))
					}
					num++
					break
				}
			}
			p.outwardLock.Unlock()
		}
	}
	return
}

func (p *proxyImpl) handlePeerSubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	p.Log(LOG_INFO, fmt.Sprintf("handlePeerSubMsg: %v", msg))
	sInfo := msg.Info
	if len(sInfo) == 0 {
		return
	}
	p.outwardLock.Lock()
	for _, sub := range sInfo {
		sub.Id, _ = sub.Id.Clone(ScopeLocal, MemberRemote)
		if p.translator != nil {
			sub.Id = p.translator.TranslateInward(sub.Id)
		}
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerSubMsg: %v", sub.Id))
		if p.filter != nil && p.filter.BlockOutward(sub.Id) {
			continue
		}
		_, ok := p.importRecvIds[sub.Id.Key()]
		if ok {
			p.outwardLock.Unlock()
			return
		}
		p.importRecvIds[sub.Id.Key()] = sub
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerSubMsg1: %v", sub.Id))
		//check if local already pubed it
		for _, pub := range p.exportSendIds {
			if pub.Id.Match(sub.Id) {
				if !p.chanTypeMatch(pub, sub) {
					err = errors.New(errRmtChanTypeMismatch)
					p.LogError(err)
					p.outwardLock.Unlock()
					return
				}
				break
			}
		}
		num++
	}
	p.outwardLock.Unlock()
	return
}

func (p *proxyImpl) handlePeerUnSubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	sInfo := msg.Info
	if len(sInfo) == 0 {
		return
	}
	p.outwardLock.Lock()
	defer p.outwardLock.Unlock()
	for _, sub := range sInfo {
		sub.Id, _ = sub.Id.Clone(ScopeLocal, MemberRemote)
		if p.translator != nil {
			sub.Id = p.translator.TranslateInward(sub.Id)
		}
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerUnSubMsg: %v", sub.Id))
		//update import cache
		delete(p.importRecvIds, sub.Id.Key())
		//check if local already pubed it
		p.appRecvChans.DelChan(sub.Id)
		num++
	}
	return
}

func (p *proxyImpl) handlePeerPubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	p.Log(LOG_INFO, fmt.Sprintf("handlePeerPubMsg: %v", msg))
	pInfo := msg.Info
	if len(pInfo) == 0 {
		return
	}
	readyInfo := make([]*ChanReadyInfo, len(pInfo))
	p.inwardLock.Lock()
	for _, pub := range pInfo {
		pub.Id, _ = pub.Id.Clone(ScopeLocal, MemberRemote)
		if p.translator != nil {
			pub.Id = p.translator.TranslateInward(pub.Id)
		}
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerPubMsg: %v", pub.Id))
		if p.filter != nil && p.filter.BlockInward(pub.Id) {
			continue
		}
		_, ok := p.importSendIds[pub.Id.Key()]
		if ok {
			p.inwardLock.Unlock()
			return
		}
		p.importSendIds[pub.Id.Key()] = pub
		for _, sub := range p.exportRecvIds {
			if pub.Id.Match(sub.Id) {
				if !p.chanTypeMatch(pub, sub) {
					err = errors.New(errRmtChanTypeMismatch)
					p.LogError(err)
					p.inwardLock.Unlock()
					return
				}
				id := pub.Id
				if p.translator != nil {
					id = p.translator.TranslateOutward(id)
				}
				p.Log(LOG_INFO, fmt.Sprintf("send ConnReady for: %v", pub.Id))
				readyInfo[num] = &ChanReadyInfo{id, p.router.recvChanBufSize(sub.Id)}
				num++
				p.appSendChans.AddSender(pub.Id, pub.ChanType)
			}
		}
	}
	p.inwardLock.Unlock()
	//send ConnReadyMsg
	if num > 0 {
		p.peer.sendCtrlMsg(&genericMsg{p.router.SysID(ReadyId), &ConnReadyMsg{Info: readyInfo[0:num]}})
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerPubMsg sends ConnReadyMsg for : %v, num : %v", readyInfo[0].Id, num))
	}
	return
}

func (p *proxyImpl) handlePeerUnPubMsg(m *genericMsg) (num int, err error) {
	msg := m.Data.(*ChanInfoMsg)
	pInfo := msg.Info
	if len(pInfo) == 0 {
		return
	}
	p.inwardLock.Lock()
	defer p.inwardLock.Unlock()
	for _, pub := range pInfo {
		pub.Id, _ = pub.Id.Clone(ScopeLocal, MemberRemote)
		if p.translator != nil {
			pub.Id = p.translator.TranslateInward(pub.Id)
		}
		p.Log(LOG_INFO, fmt.Sprintf("handlePeerUnPubMsg: %v", pub.Id))
		//update import cache
		delete(p.importSendIds, pub.Id.Key())
		p.appSendChans.DelChan(pub.Id)
		num++
	}
	return
}

func (p *proxyImpl) initSubInfoMsg() *ChanInfoMsg {
	num := len(p.exportRecvIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.exportRecvIds {
		info[idx] = new(ChanInfo)
		if p.translator != nil {
			info[idx].Id = p.translator.TranslateOutward(v.Id)
		} else {
			info[idx].Id = v.Id
		}
		info[idx].ChanType = v.ChanType
		idx++
	}
	p.Log(LOG_INFO, fmt.Sprintf("initSubInfoMsg: %v", info[0:idx]))
	return &ChanInfoMsg{Info: info[0:idx]}
}

func (p *proxyImpl) initPubInfoMsg() *ChanInfoMsg {
	num := len(p.exportSendIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.exportSendIds {
		info[idx] = new(ChanInfo)
		if p.translator != nil {
			info[idx].Id = p.translator.TranslateOutward(v.Id)
		} else {
			info[idx].Id = v.Id
		}
		info[idx].Id = v.Id
		info[idx].ChanType = v.ChanType
		idx++
	}
	p.Log(LOG_INFO, fmt.Sprintf("initPubInfoMsg: %v", info[0:idx]))
	return &ChanInfoMsg{Info: info[0:idx]}
}

func (p *proxyImpl) PeerPubInfo() []*ChanInfo {
	p.inwardLock.Lock()
	defer p.inwardLock.Unlock()
	num := len(p.importSendIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.importSendIds {
		info[idx] = new(ChanInfo)
		info[idx].Id = v.Id
		info[idx].ChanType = v.ChanType
		idx++
	}
	return info
}

func (p *proxyImpl) PeerSubInfo() []*ChanInfo {
	p.outwardLock.Lock()
	defer p.outwardLock.Unlock()
	num := len(p.importRecvIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.importRecvIds {
		info[idx] = new(ChanInfo)
		info[idx].Id = v.Id
		info[idx].ChanType = v.ChanType
		idx++
	}
	return info
}

func (p *proxyImpl) LocalPubInfo() []*ChanInfo {
	p.outwardLock.Lock()
	defer p.outwardLock.Unlock()
	num := len(p.exportSendIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.exportSendIds {
		info[idx] = new(ChanInfo)
		info[idx].Id = v.Id
		info[idx].ChanType = v.ChanType
		idx++
	}
	return info
}

func (p *proxyImpl) LocalSubInfo() []*ChanInfo {
	p.inwardLock.Lock()
	defer p.inwardLock.Unlock()
	num := len(p.exportRecvIds)
	info := make([]*ChanInfo, num)
	idx := 0
	for _, v := range p.exportRecvIds {
		info[idx] = new(ChanInfo)
		info[idx].Id = v.Id
		info[idx].ChanType = v.ChanType
		idx++
	}
	return info
}
