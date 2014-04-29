//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

/*
 "router" is a Go package for peer-peer pub/sub message passing. 
 The basic usage is to attach a send channel to an id in router to send messages, 
 and attach a recv channel to an id to receive messages. If these 2 ids match, 
 the messages from send channel will be "routed" to recv channel, e.g.

    rot := router.New(...)
    chan1 := make(chan string)
    chan2 := make(chan string)
    chan3 := make(chan string)
    rot.AttachSendChan(PathID("/sports/basketball"), chan1)
    rot.AttachRecvChan(PathID("/sports/basketball"), chan2)
    rot.AttachRecvChan(PathID("/sports/*"), chan3)

 We can use integers, strings, pathnames, or structs as Ids in router (maybe regex ids
 and tuple id in future).

 we can connect two routers so that channels attached to router1 can communicate with
 channels attached to router2 transparently.
*/
package router

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
)

//Default size settings in router
const (
	DefLogBufSize      = 256
	DefDataChanBufSize = 32
	DefCmdChanBufSize  = 64
	UnlimitedBuffer    = -1
)

//Router is the main access point to functionality. Applications will create an instance
//of it thru router.New(...) and attach channels to it
type Router interface {
	//---- core api ----
	//Attach chans to id in router, with an optional argument (chan *BindEvent)
	//When specified, the optional argument will serve two purposes:
	//1. used to tell when the remote peers connecting/disconn
	//2. in AttachRecvChan, used as a flag to ask router to keep recv chan open when all senders close
	//the returned RoutedChan object can be used to find the number of bound peers: routCh.NumPeers()
	AttachSendChan(Id, interface{}, ...interface{}) (*RoutedChan, error)
	//3. When attaching recv chans, an optional integer can specify the internal buffering size
	AttachRecvChan(Id, interface{}, ...interface{}) (*RoutedChan, error)

	//Detach sendChan/recvChan from router
	DetachChan(Id, interface{}) error

	//Shutdown router, and close attached proxies and chans
	Close()

	//Connect this router to another router.
	//1. internally it calls Proxy.Connect(...) to do the real job
	//2. The connection can be disconnected by calling Proxy.Close() on returned proxy object
	//3. for more compilcated connection setup (such as setting IdFilter and IdTranslator), use Proxy.Connect() instead

	//Connect to a local router
	Connect(Router) (Proxy, Proxy, error)

	//Connect to a remote router thru io conn
	//1. io.ReadWriteCloser: transport connection
	//2. MarshalingPolicy: gob or json marshaling
	//3. remaining args can be a FlowControlPolicy (e.g. window based or XOnOff)
	ConnectRemote(io.ReadWriteCloser, MarshalingPolicy, ...interface{}) (Proxy, error)

	//--- other utils ---
	//return pre-created SysIds according to the router's id-type, with ScopeGlobal / MemberLocal
	SysID(idx int) Id

	//create a new SysId with "args..." specifying scope/membership
	NewSysID(idx int, args ...int) Id

	//return all ids and their ChanTypes from router's namespace which satisfy predicate
	IdsForSend(predicate func(id Id) bool) map[interface{}]*ChanInfo
	IdsForRecv(predicate func(id Id) bool) map[interface{}]*ChanInfo
}

//Major data structures for router:
//1. tblEntry: an entry for each id in router
//2. routerImpl: main data struct of router
type tblEntry struct {
	chanType reflect.Type
	id       Id
	senders  map[interface{}]*RoutedChan
	recvers  map[interface{}]*RoutedChan
}

type routerImpl struct {
	async          bool
	closed         bool
	defChanBufSize int
	dispPolicy     DispatchPolicy
	seedId         Id
	idType         reflect.Type
	matchType      MatchType
	tblLock        sync.Mutex
	routingTable   map[interface{}](*tblEntry)
	sysIds         [NumSysInternalIds]Id
	notifier       *notifier
	proxLock       sync.Mutex
	proxies        []Proxy
	bufSizeLock    sync.Mutex
	recvBufSizes   map[interface{}]int
	//for log/debug, if name != nil, debug is enabled
	Logger
	LogSink
	FaultRaiser
	name string
}

func (s *routerImpl) NewSysID(idx int, args ...int) Id {
	sid, err := s.seedId.SysID(idx, args...)
	if err != nil {
		s.LogError(err)
		return nil
	}
	return sid
}

func (s *routerImpl) SysID(indx int) Id {
	if indx < 0 || indx >= NumSysInternalIds {
		return nil
	}
	return s.sysIds[indx]
}

func (s *routerImpl) IdsForSend(predicate func(id Id) bool) map[interface{}]*ChanInfo {
	ids := make(map[interface{}]*ChanInfo)
	s.tblLock.Lock()
	defer s.tblLock.Unlock()
	for _, v := range s.routingTable {
		for _, e := range v.senders {
			idx := e.Id.SysIdIndex()
			if idx < 0 && predicate(e.Id) {
				ids[e.Id.Key()] = &ChanInfo{Id: e.Id, ChanType: v.chanType}
			}
		}
	}
	return ids
}

func (s *routerImpl) IdsForRecv(predicate func(id Id) bool) map[interface{}]*ChanInfo {
	ids := make(map[interface{}]*ChanInfo)
	s.tblLock.Lock()
	defer s.tblLock.Unlock()
	for _, v := range s.routingTable {
		for _, e := range v.recvers {
			idx := e.Id.SysIdIndex()
			if idx < 0 && predicate(e.Id) {
				ids[e.Id.Key()] = &ChanInfo{Id: e.Id, ChanType: v.chanType}
			}
		}
	}
	return ids
}

func (s *routerImpl) validateId(id Id) (err error) {
	if id == nil || (id.Scope() < ScopeGlobal || id.Scope() > ScopeLocal) ||
		(id.Member() < MemberLocal || id.Member() > MemberRemote) {
		err = errors.New(fmt.Sprintf("%s: %v", errInvalidId, id))
	}
	return
}

// could use one optional argument of (chan *BindEvent), used to notify if peer recv chan has attached
func (s *routerImpl) AttachSendChan(id Id, v interface{}, args ...interface{}) (routCh *RoutedChan, err error) {
	if err = s.validateId(id); err != nil {
		s.LogError(err)
		//s.Raise(err)
		return
	}
	ch, internalChan := v.(Channel)
	if !internalChan {
		ch1 := reflect.ValueOf(v)
		if ch1.Kind() != reflect.Chan {
			err = errors.New(errInvalidChan)
			s.LogError(err)
			//s.Raise(err)
			return
		}
		ch = ch1
	}
	l := len(args)
	var bindChan chan *BindEvent
	if l > 0 {
		switch cv := args[0].(type) {
		case chan *BindEvent:
			bindChan = cv
			if cap(bindChan) == 0 {
				err = errors.New(errInvalidBindChan + ": binding bindChan is not buffered")
				s.LogError(err)
				//s.Raise(err)
				return
			}
		default:
			err = errors.New("invalid arguments to attach send chan")
			s.LogError(err)
			//s.Raise(err)
			return
		}
	}
	routCh = newRoutedChan(id, reflect.SendDir, ch, s, bindChan)
	routCh.internalChan = internalChan
	err = s.attach(routCh)
	if err != nil {
		s.LogError(err)
		//s.Raise(err)
	}
	return
}

// could use two optional arguments:
//    1. chan of *BindEvent: serve two purposes:
//           1> notify if peer send chan has attached
//           2> ask router to keep this recv chan open even if all peer send chans closed, used for persistent servers
//    2> an integer: the size of internal buffering inside router for this recv chan
func (s *routerImpl) AttachRecvChan(id Id, v interface{}, args ...interface{}) (routCh *RoutedChan, err error) {
	if err = s.validateId(id); err != nil {
		s.LogError(err)
		//s.Raise(err)
		return
	}
	ch, internalChan := v.(Channel)
	if !internalChan {
		ch1 := reflect.ValueOf(v)
		if ch1.Kind() != reflect.Chan {
			err = errors.New(errInvalidChan)
			s.LogError(err)
			//s.Raise(err)
			return
		}
		ch = ch1
	}
	var bindChan chan *BindEvent
	for i := 0; i < len(args); i++ {
		switch cv := args[i].(type) {
		case chan *BindEvent:
			bindChan = cv
			if cap(bindChan) == 0 {
				err = errors.New(errInvalidBindChan + ": binding bindChan is not buffered")
				s.LogError(err)
				//s.Raise(err)
				return
			}
		case int:
			//set recv chan buffer size
			s.bufSizeLock.Lock()
			old, ok := s.recvBufSizes[id.Key()]
			if !ok || old < cv {
				s.recvBufSizes[id.Key()] = cv
			}
			s.bufSizeLock.Unlock()
		default:
			err = errors.New("invalid arguments to attach recv chan")
			s.LogError(err)
			//s.Raise(err)
			return
		}
	}
	if s.async && ch.Cap() != UnlimitedBuffer && !internalChan {
		//for async router, external recv chans must have unlimited buffering, 
		//ie. Cap()==-1, all undelivered msgs will be buffered right before ext recv chans
		ch = &asyncChan{Channel: ch}
	}
	routCh = newRoutedChan(id, reflect.RecvDir, ch, s, bindChan)
	routCh.internalChan = internalChan
	err = s.attach(routCh)
	if err != nil {
		s.LogError(err)
		//s.Raise(err)
	}
	return
}

func (s *routerImpl) DetachChan(id Id, v interface{}) (err error) {
	//s.Log(LOG_INFO, "DetachChan called...")
	if err = s.validateId(id); err != nil {
		s.LogError(err)
		//s.Raise(err)
		return
	}
	ch, ok := v.(Channel)
	if !ok {
		ch1 := reflect.ValueOf(v)
		if ch1.Kind() != reflect.Chan {
			err = errors.New(errInvalidChan)
			s.LogError(err)
			//s.Raise(err)
			return
		}
		ch = ch1
	}
	routCh := &RoutedChan{}
	routCh.Id = id
	routCh.Channel = ch
	routCh.router = s
	err = s.detach(routCh, false)
	return
}

func (s *routerImpl) Close() {
	s.Log(LOG_INFO, "Close()/shutdown called")
	s.shutdown()
}

func (s *routerImpl) attach(routCh *RoutedChan) (err error) {
	//handle id
	if reflect.TypeOf(routCh.Id) != s.idType {
		err = errors.New(errIdTypeMismatch + ": " + routCh.Id.String())
		s.LogError(err)
		return
	}

	s.tblLock.Lock()

	if s.closed {
		s.tblLock.Unlock()
		err = errors.New(fmt.Sprintf("Router closed, cannot attach chan for id %v", routCh.Id))
		s.LogError(err)
		return
	}

	//router entry
	ent, ok := s.routingTable[routCh.Id.Key()]
	if !ok {
		if routCh.internalChan {
			err = errors.New(fmt.Sprintf("%s %v", errChanGenericType, routCh.Id))
			s.LogError(err)
			s.tblLock.Unlock()
			return
		}
		//first routedChan attached to this id, add a router-entry for this id
		ent = &tblEntry{}
		s.routingTable[routCh.Id.Key()] = ent
		ent.id = routCh.Id // will only use the Val/Match() part of id
		ent.chanType = routCh.Channel.Type()
		ent.senders = make(map[interface{}]*RoutedChan)
		ent.recvers = make(map[interface{}]*RoutedChan)
	} else {
		if !routCh.internalChan && routCh.Channel.Type() != ent.chanType {
			err = errors.New(fmt.Sprintf("%s %v", errChanTypeMismatch, routCh.Id))
			s.LogError(err)
			s.tblLock.Unlock()
			return
		}
	}

	//check for duplicate
	switch routCh.Dir {
	case reflect.SendDir:
		if _, ok = ent.senders[routCh.Channel.Interface()]; ok {
			err = errors.New(errDupAttachment)
			s.LogError(err)
			s.tblLock.Unlock()
			return
		} else {
			ent.senders[routCh.Channel.Interface()] = routCh
		}
	case reflect.RecvDir:
		if _, ok = ent.recvers[routCh.Channel.Interface()]; ok {
			err = errors.New(errDupAttachment)
			s.LogError(err)
			s.tblLock.Unlock()
			return
		} else {
			ent.recvers[routCh.Channel.Interface()] = routCh
		}
	}

	idx := routCh.Id.SysIdIndex()
	var matches []*RoutedChan

	//find bindings for routedChan
	if s.matchType == ExactMatch {
		switch routCh.Dir {
		case reflect.SendDir:
			for _, recver := range ent.recvers {
				if scope_match(routCh.Id, recver.Id) {
					s.Log(LOG_INFO, fmt.Sprintf("add bindings: %v -> %v", routCh.Id, recver.Id))
					matches = append(matches, recver)
				}
			}
		case reflect.RecvDir:
			for _, sender := range ent.senders {
				if scope_match(sender.Id, routCh.Id) {
					s.Log(LOG_INFO, fmt.Sprintf("add bindings: %v -> %v", sender.Id, routCh.Id))
					matches = append(matches, sender)
				}
			}
		}
	} else { //for PrefixMatch & AssocMatch, need to iterate thru all entries in map routingTable
		for _, ent2 := range s.routingTable {
			if routCh.Id.Match(ent2.id) {
				if routCh.Channel.Type() == ent2.chanType ||
					(routCh.Dir == reflect.RecvDir && routCh.internalChan) {
					switch routCh.Dir {
					case reflect.SendDir:
						for _, recver := range ent2.recvers {
							if scope_match(routCh.Id, recver.Id) {
								s.Log(LOG_INFO, fmt.Sprintf("add bindings: %v -> %v", routCh.Id, recver.Id))
								matches = append(matches, recver)
							}
						}
					case reflect.RecvDir:
						for _, sender := range ent2.senders {
							if scope_match(sender.Id, routCh.Id) {
								s.Log(LOG_INFO, fmt.Sprintf("add bindings: %v -> %v", sender.Id, routCh.Id))
								matches = append(matches, sender)
							}
						}
					}
				} else {
					em := errors.New(fmt.Sprintf("%s : [%v, %v]", errChanTypeMismatch, routCh.Id, ent2.id))
					s.Log(LOG_ERROR, em)
					//should crash here?
					//s.Raise(em)
					return
				}
			}
		}
	}

	s.tblLock.Unlock()

	//finished updating routing table
	for i := 0; i < len(matches); i++ {
		peer := matches[i]
		if routCh.Dir == reflect.SendDir {
			routCh.attach(peer)
		} else {
			peer.attach(routCh)
		}
	}

	//activate
	//force broadcaster for system ids
	if idx >= 0 { //sys ids
		routCh.start(BroadcastPolicy)
	} else {
		routCh.start(s.dispPolicy)
	}

	//notifier will send in a separate goroutine, so non-blocking here
	if idx < 0 && routCh.Id.Member() == MemberLocal { //not sys ids
		switch routCh.Dir {
		case reflect.SendDir:
			s.notifier.notify(PubId, &ChanInfo{Id: routCh.Id, ChanType: routCh.Channel.Type()})
		case reflect.RecvDir:
			s.notifier.notify(SubId, &ChanInfo{Id: routCh.Id, ChanType: routCh.Channel.Type()})
		}
	}
	return
}

func (s *routerImpl) detach(routCh *RoutedChan, bySelf bool) (err error) {
	//s.Log(LOG_INFO, fmt.Sprintf("detach chan from id %v\n", routCh.Id))

	//check id
	if reflect.TypeOf(routCh.Id) != s.idType {
		err = errors.New(errIdTypeMismatch + ": " + routCh.Id.String())
		s.LogError(err)
		return
	}

	s.tblLock.Lock()

	//find router entry
	ent, ok := s.routingTable[routCh.Id.Key()]
	if !ok {
		err = errors.New(errDetachChanNotInRouter + ": " + routCh.Id.String())
		s.LogError(err)
		s.tblLock.Unlock()
		return
	}

	//find the routedChan & remove it from tblEntry
	routCh1, ok := ent.senders[routCh.Channel.Interface()]
	if !ok {
		routCh1, ok = ent.recvers[routCh.Channel.Interface()]
	}

	s.tblLock.Unlock()

	if !ok {
		err = errors.New(errDetachChanNotInRouter + ": " + routCh.Id.String())
		s.LogError(err)
		return
	}

	//force calling router.detach() from forwarding goroutine
	if routCh1.Dir == reflect.SendDir && !bySelf {
		routCh1.Close()
		return
	}

	s.tblLock.Lock()

	if routCh1.Dir == reflect.SendDir {
		delete(ent.senders, routCh.Channel.Interface())
	} else {
		delete(ent.recvers, routCh.Channel.Interface())
	}

	s.tblLock.Unlock()

	//mark routCh1 to be detached
	if routCh1.Dir == reflect.RecvDir {
		routCh1.bindLock.Lock()
		if !routCh1.detached {
			routCh1.detached = true
		}
		routCh1.bindLock.Unlock()
	}

	//remove bindings from peers. dup bindings to avoid race at shutdown
	//and detach recver from sender first
	copySet := routCh1.Peers()
	for _, v := range copySet {
		if routCh1.Dir == reflect.SendDir {
			routCh1.detach(v)
		} else {
			v.detach(routCh1)
		}
	}

	//notifier will send in a separate goroutine, so non-blocking here
	s.tblLock.Lock()
	routerClosed := s.closed
	s.tblLock.Unlock()
	idx := routCh1.Id.SysIdIndex()
	if !routerClosed && idx < 0 && routCh1.Id.Member() == MemberLocal { //not sys ids
		switch routCh1.Dir {
		case reflect.SendDir:
			s.notifier.notify(UnPubId, &ChanInfo{Id: routCh1.Id, ChanType: routCh1.Channel.Type()})
		case reflect.RecvDir:
			s.notifier.notify(UnSubId, &ChanInfo{Id: routCh1.Id, ChanType: routCh1.Channel.Type()})
		}
	}

	return
}

func (s *routerImpl) shutdown() {
	s.Log(LOG_INFO, "shutdown start...")

	s.tblLock.Lock()
	closed := s.closed
	if !closed {
		s.closed = true
	}
	s.tblLock.Unlock()

	if closed {
		return
	}

	// close all peers
	s.proxLock.Lock()
	//need this?
	s.proxLock.Unlock()
	//s.proxLock.Lock()
	for i := 0; i < len(s.proxies); i++ {
		s.proxies[i].Close()
	}
	//s.proxLock.Unlock()
	s.Log(LOG_INFO, "all proxy closed")

	//detach and close all senders
	s.tblLock.Lock()
	var senders []*RoutedChan
	for _, ent := range s.routingTable {
		for _, sender := range ent.senders {
			senders = append(senders, sender)
			//sender.Close()
			//sender.Detach()
		}
	}
	s.tblLock.Unlock()
	for _, snd := range senders {
		idx := snd.Id.SysIdIndex()
		//if idx != RouterLogId && idx != RouterFaultId {
		if idx < 0 { //not sys ids
			snd.Close()
		}
	}

	//wait for console log goroutine to exit
	s.FaultRaiser.Close()
	s.Logger.Close()
	s.LogSink.Close()
	s.notifier.Close()
}

func (s *routerImpl) initSysIds() {
	for i := 0; i < NumSysInternalIds; i++ {
		s.sysIds[i], _ = s.seedId.SysID(i)
	}
}

func (s *routerImpl) recvChanBufSize(id Id) int {
	s.bufSizeLock.Lock()
	defer s.bufSizeLock.Unlock()
	v, ok := s.recvBufSizes[id.Key()]
	if ok {
		return v
	}
	return s.defChanBufSize
}

func (s *routerImpl) addProxy(p Proxy) {
	s.Log(LOG_INFO, "add proxy")
	s.proxLock.Lock()
	s.proxies = append(s.proxies, p)
	s.proxLock.Unlock()
}

func (s *routerImpl) delProxy(p Proxy) {
	s.Log(LOG_INFO, "del proxy impl")
	num := -1
	s.proxLock.Lock()
	for i := 0; i < len(s.proxies); i++ {
		if s.proxies[i] == p {
			num = i
			break
		}
	}
	if num >= 0 {
		s.proxies[num] = nil
		s.proxies = append(s.proxies[:num], s.proxies[num+1:]...)
	}
	s.proxLock.Unlock()
}

//Connect() connects this router to peer router, the real job is done inside Proxy
func (r1 *routerImpl) Connect(r2 Router) (p1, p2 Proxy, err error) {
	p1 = NewProxy(r1, "", nil, nil)
	p2 = NewProxy(r2, "", nil, nil)
	err = p1.Connect(p2)
	return
}

func (r *routerImpl) ConnectRemote(rwc io.ReadWriteCloser, mar MarshalingPolicy, args ...interface{}) (p Proxy, err error) {
	p = NewProxy(r, "", nil, nil)
	err = p.ConnectRemote(rwc, mar, args...)
	return
}

/*
 New is router constructor. It accepts the following arguments:
    1. seedId: a dummy id to show what type of ids will be used. New ids will be type-checked against this.
    2. bufSize: the buffer size used for router's internal channels.
       if bufSize >= 0, its value will be used
       if bufSize < 0, it means unlimited buffering, so router is async and sending on attached channels will never block
    3. disp: dispatch policy for router. by default, it is BroadcastPolicy
    4. optional arguments ...:
       name:     router's name, if name is defined, router internal logging will be turned on, ie LogRecord generated
       LogScope: if this is set, a console log sink is installed to show router internal log
          if logScope == ScopeLocal, only log msgs from local router will show up
          if logScope == ScopeGlobal, all log msgs from connected routers will show up
*/
func New(seedId Id, bufSize int, disp DispatchPolicy, args ...interface{}) Router {
	//parse optional router name and flag for enable console logging
	var name string
	consoleLogScope := -1
	l := len(args)
	if l > 0 {
		if sv, ok := args[0].(string); !ok {
			return nil
		} else {
			name = sv
		}
	}
	if l > 1 {
		if iv, ok := args[1].(int); !ok {
			return nil
		} else {
			consoleLogScope = iv
			if consoleLogScope < ScopeGlobal || consoleLogScope > ScopeLocal {
				return nil
			}
		}
	}
	//create a new router
	router := &routerImpl{}
	router.name = name
	router.seedId = seedId
	router.idType = reflect.TypeOf(router.seedId)
	router.matchType = router.seedId.MatchType()
	router.initSysIds()
	router.defChanBufSize = DefDataChanBufSize
	if bufSize >= 0 {
		router.defChanBufSize = bufSize
	} else {
		router.async = true
	}
	router.dispPolicy = disp
	router.routingTable = make(map[interface{}](*tblEntry))
	router.recvBufSizes = make(map[interface{}]int)
	router.notifier = newNotifier(router)
	router.Logger.Init(router.SysID(RouterLogId), router, router.name)
	if consoleLogScope >= ScopeGlobal && consoleLogScope <= ScopeLocal {
		router.LogSink.Init(router.NewSysID(RouterLogId, consoleLogScope), router)
	}
	router.FaultRaiser.Init(router.SysID(RouterFaultId), router, router.name)
	return router
}
