//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"fmt"
	"reflect"
)

//a message struct for propagating router's namespace changes (chan attachments or detachments)
type ChanInfoMsg struct {
	Info []*ChanInfo
}

//a message struct holding information about id and its associated ChanType
type ChanInfo struct {
	Id       Id
	ChanType reflect.Type
	ElemType *chanElemTypeData
}

func (ici ChanInfo) String() string {
	return fmt.Sprintf("%v_%v", ici.Id, ici.ElemType)
}

//store marshaled information about ChanType
type chanElemTypeData struct {
	//full name of elem type: pkg_path.data_type
	FullName string
	//the following should be string encoding of the elem type
	//it contains info for both names/types.
	//e.g. for struct, it could be in form of "struct{fieldName:typeName,...}"
	TypeEncoding string
}

func (et chanElemTypeData) String() string {
	return fmt.Sprintf("[%v_%v]", et.FullName, et.TypeEncoding)
}

//the generic message wrapper
type genericMsg struct {
	Id   Id
	Data interface{}
}

//a message struct containing information about remote router connection
type ConnInfoMsg struct {
	ConnInfo string
	Error    string
	Id       Id
	Type     string //async/flowControlled/raw
}

//recver-router notify sender-router which channel are ready to recv how many msgs
type ChanReadyInfo struct {
	Id     Id
	Credit int
}

func (cri ChanReadyInfo) String() string {
	return fmt.Sprintf("%v_%v", cri.Id, cri.Credit)
}

type ConnReadyMsg struct {
	Info []*ChanReadyInfo
}

type BindEventType int8

const (
	PeerAttach BindEventType = iota
	PeerDetach
	EndOfData
)

//a message struct containing information for peer (sender/recver) binding/connection.
//sent by router whenever peer attached or detached.
//   Type:  the type of event just happened: PeerAttach/PeerDetach/EndOfData
//   Count: how many peers are still bound now
type BindEvent struct {
	Type  BindEventType
	Count int //total attached
}
