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

type stream struct {
	peer            peerIntf
	outputChan      chan *genericMsg //outputMainLoop serve this chan
	outputAsyncChan *asyncChan       //wrap outputChan to give it unlimited buffering
	//
	rwc   io.ReadWriteCloser
	mar   Marshaler
	demar Demarshaler
	//
	proxy *proxyImpl
	//others
	Logger
	FaultRaiser
	Closed    bool
	numSender int
	sync.Mutex
}

func newStream(rwc io.ReadWriteCloser, mp MarshalingPolicy, p *proxyImpl) *stream {
	s := new(stream)
	s.proxy = p
	//
	s.outputChan = make(chan *genericMsg, s.proxy.router.defChanBufSize+DefCmdChanBufSize)
	s.outputAsyncChan = &asyncChan{Channel: reflect.ValueOf(s.outputChan)}
	s.rwc = rwc
	mp.Register(s.proxy.router.seedId)
	s.mar = mp.NewMarshaler(rwc)
	s.demar = mp.NewDemarshaler(rwc)
	//
	ln := ""
	if len(p.router.name) > 0 {
		if len(p.name) > 0 {
			ln = p.router.name + p.name
		} else {
			ln = p.router.name + "_proxy"
		}
		ln += "_stream"
	}
	s.Logger.Init(p.router.SysID(RouterLogId), p.router, ln)
	s.FaultRaiser.Init(p.router.SysID(RouterFaultId), p.router, ln)
	return s
}

func (s *stream) start() {
	go s.outputMainLoop()
	go s.inputMainLoop()
}

func (s *stream) Close() {
	s.Lock()
	defer s.Unlock()
	if !s.Closed {
		s.Log(LOG_INFO, "Close() is called")
		//notify peer
		s.Closed = true
		s.peer.sendCtrlMsg(&genericMsg{s.proxy.router.SysID(DisconnId), &ConnInfoMsg{}})
		//shutdown inputMainLoop
		s.rwc.Close()
		//close logger
		s.FaultRaiser.Close()
		s.Logger.Close()
	}
}

//when concating channel adpaters: asyncChan, genMsgChan, attach asyncChan first
//so all outgoing channels share the same asyncChan (its buffer and forwarder)
//there will only 1 forwarder for each stream connection
func (s *stream) appMsgChanForId(id Id) (Channel, int) {
	var appCh Channel
	if s.proxy.translator != nil {
		if s.proxy.router.async || s.proxy.flowController != nil {
			appCh = newGenMsgChan(s.proxy.translator.TranslateOutward(id), s.outputAsyncChan)
		} else {
			appCh = newGenericMsgChan(s.proxy.translator.TranslateOutward(id), s.outputChan)
		}
	} else {
		if s.proxy.router.async || s.proxy.flowController != nil {
			appCh = newGenMsgChan(id, s.outputAsyncChan)
		} else {
			appCh = newGenericMsgChan(id, s.outputChan)
		}
	}
	s.Lock()
	s.numSender++
	s.Unlock()
	return appCh, 1
}

//send ctrl data to io.Writer
func (s *stream) sendCtrlMsg(m *genericMsg) (err error) {
	s.outputChan <- m
	if m.Id.SysIdIndex() == DisconnId {
		s.Close()
	}
	return
}

func (s *stream) outputMainLoop() {
	s.Log(LOG_INFO, "stream outputMainLoop start")
	//
	var err error
	cont := true
	for cont {
		m, oOpen := <-s.outputChan
		if !oOpen {
			cont = false
		} else {
			if m.Id.Scope() == NumScope && m.Id.Member() == NumMembership {
				s.Lock()
				s.numSender--
				if s.numSender == 0 && s.Closed {
					cont = false
				}
				s.Unlock()
				if !cont {
					break
				}
			}
			//send id
			if err = s.mar.Marshal(m.Id); err != nil {
				s.LogError(err)
				cont = false
			} else if !(m.Id.Scope() == NumScope && m.Id.Member() == NumMembership) {
				//for json encoding, we need pre-create id structs saved as interface
				//in messages; so send length of message first; so we can reconstruct
				//the array at recv side
				switch m.Id.SysIdIndex() {
				case PubId, UnPubId, SubId, UnSubId:
					ici := m.Data.(*ChanInfoMsg)
					if err = marshalIdChanInfoMsg(s.mar, ici); err != nil {
						s.LogError(err)
						cont = false
					}
				case ReadyId:
					ici := m.Data.(*ConnReadyMsg)
					if err = marshalConnReadyMsg(s.mar, ici); err != nil {
						s.LogError(err)
						cont = false
					}
				default:
					//send data
					if err = s.mar.Marshal(m.Data); err != nil {
						s.LogError(err)
						cont = false
					}
				}
			}
		}
	}
	if err != nil {
		//must be io conn fail or marshal fail
		//notify proxy disconn
		s.peer.sendCtrlMsg(&genericMsg{s.proxy.router.SysID(DisconnId), &ConnInfoMsg{}})
	}
	s.Log(LOG_INFO, "stream outputMainLoop exit")
	s.Close()
}

//read data from io.Reader, pass ctrlMsg to exportCtrlChan and dataMsg to peer
func (s *stream) inputMainLoop() {
	s.Log(LOG_INFO, "stream inputMainLoop start")
	cont := true
	for cont {
		if err := s.recv(); err != nil {
			cont = false
		}
	}
	s.Log(LOG_INFO, "stream inputMainLoop exit")
	//when reach here, must be io conn fail or demarshal fail
	s.Close()
}

func (s *stream) recv() (err error) {
	r := s.proxy.router
	id, _ := r.seedId.Clone()
	if err = s.demar.Demarshal(id); err != nil {
		s.LogError(err)
		return
	}
	switch id.SysIdIndex() {
	case ConnId, DisconnId, ErrorId:
		id1, _ := r.seedId.Clone()
		cm := &ConnInfoMsg{Id: id1}
		err = s.demar.Demarshal(cm)
		if err != nil {
			s.LogError(err)
			return
		} else {
			s.peer.sendCtrlMsg(&genericMsg{id, cm})
		}
	case ReadyId:
		cm := &ConnReadyMsg{}
		err = demarshalConnReadyMsg(s.demar, id, cm)
		if err != nil {
			s.LogError(err)
			return
		} else {
			s.peer.sendCtrlMsg(&genericMsg{id, cm})
		}
	case PubId, UnPubId, SubId, UnSubId:
		cm := &ChanInfoMsg{}
		err = demarshalIdChanInfoMsg(s.demar, id, cm)
		if err != nil {
			s.LogError(err)
			return
		} else {
			s.peer.sendCtrlMsg(&genericMsg{id, cm})
		}
	default: //appMsg
		peerChan, num := s.peer.appMsgChanForId(id)
		if peerChan == nil {
			err = errors.New(fmt.Sprintf("Stream fail to find sendChan for id %v", id))
			s.LogError(err)
			return
		}
		if id.Scope() == NumScope && id.Member() == NumMembership { //chan is closed
			peerChan.Close()
			s.Log(LOG_INFO, fmt.Sprintf("close proxy forwarding chan for %v", id))
			return
		}
		chanType := peerChan.Type()
		if chanType == nil {
			err = errors.New(fmt.Sprintf("failed to find chanType for id %v", id))
			return
		}
		appMsg := reflect.New(chanType.Elem())
		err = s.demar.Demarshal(appMsg.Interface())
		if err != nil {
			s.LogError(err)
			return
		} else {
			if num > 0 {
				peerChan.Send(appMsg.Elem())
			}
		}
	}
	return
}
