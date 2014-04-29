//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
)

//the common interface of all marshaler such as GobMarshaler and JsonMarshaler
type Marshaler interface {
	Marshal(interface{}) error
}

//the common interface of all demarshaler such as GobDemarshaler and JsonDemarshaler
type Demarshaler interface {
	Demarshal(interface{}) error
}

//the common interface of all Marshaling policy such as GobMarshaling and JsonMarshaling
type MarshalingPolicy interface {
	NewMarshaler(io.Writer) Marshaler
	NewDemarshaler(io.Reader) Demarshaler
	Register(interface{})
}

// marshalling policy using gob

type gobMarshalingPolicy struct {
	registry map[interface{}]bool
	sync.Mutex
}

type gobMarshaler struct {
	*gob.Encoder
}
type gobDemarshaler struct {
	*gob.Decoder
}

//use package "gob" for marshaling
var GobMarshaling MarshalingPolicy = &gobMarshalingPolicy{registry: make(map[interface{}]bool)}

func (g *gobMarshalingPolicy) Register(t interface{}) {
	//register internal concrete types for messages interfaces
	g.Lock()
	defer g.Unlock()
	found := g.registry[t]
	if !found {
		gob.Register(t)
		g.registry[t] = true
	}
}

func (g *gobMarshalingPolicy) NewMarshaler(w io.Writer) Marshaler {
	return &gobMarshaler{gob.NewEncoder(w)}
}

func (g *gobMarshalingPolicy) NewDemarshaler(r io.Reader) Demarshaler {
	return &gobDemarshaler{gob.NewDecoder(r)}
}

func (gm *gobMarshaler) Marshal(e interface{}) error {
	return gm.Encode(e)
}

func (gm *gobDemarshaler) Demarshal(e interface{}) error {
	return gm.Decode(e)
}

// marshalling policy using json

type jsonMarshalingPolicy byte
type jsonMarshaler struct {
	*json.Encoder
}
type jsonDemarshaler struct {
	*json.Decoder
}

//use package "json" for marshaling
var JsonMarshaling MarshalingPolicy = jsonMarshalingPolicy(1)

func (j jsonMarshalingPolicy) Register(t interface{}) {
	// do nothing
}

func (j jsonMarshalingPolicy) NewMarshaler(w io.Writer) Marshaler {
	return &jsonMarshaler{json.NewEncoder(w)}
}

func (j jsonMarshalingPolicy) NewDemarshaler(r io.Reader) Demarshaler {
	return &jsonDemarshaler{json.NewDecoder(r)}
}

func (jm *jsonMarshaler) Marshal(e interface{}) error {
	return jm.Encode(e)
}

func (jm *jsonDemarshaler) Demarshal(e interface{}) error {
	return jm.Decode(e)
}

func marshalConnReadyMsg(mar Marshaler, crm *ConnReadyMsg) (err error) {
	sz := len(crm.Info)
	if err = mar.Marshal(sz); err != nil {
		return
	}
	return mar.Marshal(crm)
}

func demarshalConnReadyMsg(demar Demarshaler, id Id, crm *ConnReadyMsg) (err error) {
	num := 0
	if err = demar.Demarshal(&num); err != nil {
		return
	}
	if num > 0 {
		info := make([]*ChanReadyInfo, num)
		for i := 0; i < num; i++ {
			id1, _ := id.Clone()
			info[i] = &ChanReadyInfo{Id: id1}
		}
		crm.Info = info
	}
	return demar.Demarshal(crm)
}

func marshalIdChanInfoMsg(mar Marshaler, crm *ChanInfoMsg) (err error) {
	sz := len(crm.Info)
	if err = mar.Marshal(sz); err != nil {
		return
	}
	for i := 0; i < sz; i++ {
		ici := crm.Info[i]
		if err = mar.Marshal(ici.Id); err != nil {
			//fmt.Printf("marshalIdChanInfoMsg: failed to marshal Id=%v, err=%v\n", ici.Id, err)
			//return
		}
		if ici.ElemType == nil {
			ici.ElemType = new(chanElemTypeData)
		}
		if len(ici.ElemType.FullName) == 0 {
			ici.ElemType.FullName = getMsgTypeEncoding(ici.ChanType.Elem())
		}
		if err = mar.Marshal(ici.ElemType); err != nil {
			//fmt.Printf("marshalIdChanInfoMsg: failed to marshal ElemType=%v, err=%v\n", ici.ElemType, err)
			//return
		}
	}
	return
}

func demarshalIdChanInfoMsg(demar Demarshaler, id Id, crm *ChanInfoMsg) (err error) {
	num := 0
	if err = demar.Demarshal(&num); err != nil {
		return
	}
	if num > 0 {
		info := make([]*ChanInfo, num)
		for i := 0; i < num; i++ {
			id1, _ := id.Clone()
			info[i] = &ChanInfo{Id: id1, ElemType: &chanElemTypeData{}}
			if err = demar.Demarshal(info[i].Id); err != nil {
				//fmt.Printf("marshalIdChanInfoMsg: failed to demarshal Id=%v, err=%v\n", info[i].Id, err)
				//return
			}
			if err = demar.Demarshal(info[i].ElemType); err != nil {
				//fmt.Printf("marshalIdChanInfoMsg: failed to demarshal ElemType=%v, err=%v\n", info[i].ElemType, err)
				//return
			}
		}
		crm.Info = info
	}
	return
}

func baseType(t reflect.Type) (b reflect.Type) {
	b = t
	defer func() {
		_ = recover()
	}()
	for {
		b = b.Elem()
	}
	return
}

func getMsgTypeEncoding(t reflect.Type) string {
	b := baseType(t)
	return fmt.Sprintf("%v.%v.%v", b.PkgPath(), b.Name(), t.Kind())
}
