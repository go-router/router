//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"errors"
	"fmt"
	"strconv"
)

//Membership identifies whether communicating peers (send chans and recv chans) are from the same router or diff routers
const (
	MemberLocal  = iota //peers (send chans and recv chans) are from the same router
	MemberRemote        //peers (send chans and recv chans) are from diff routers
	NumMembership
)

//return the string values of membership
var memberString []string = []string{"MemberLocal", "MemberRemote", "InvalidMembership"}

//Scope is the scope to publish/subscribe (or send/recv) msgs
const (
	ScopeGlobal = iota // send to or recv from both local and remote peers
	ScopeRemote        // send to or recv from remote peers
	ScopeLocal         // send to or recv from local peers
	NumScope
)

//return string values of Scope
var scopeString []string = []string{"ScopeLocal", "ScopeRemote", "ScopeGlobal"}

//MatchType describes the types of namespaces and match algorithms used for id-matching
type MatchType int

const (
	ExactMatch  MatchType = iota
	PrefixMatch           // for PathId
	AssocMatch            // for RegexId, TupleId
)

//Id defines the common interface shared by all kinds of ids: integers/strings/pathnames...
type Id interface {
	//methods to query Id content
	Scope() int
	Member() int

	//key value for storing Id in map
	Key() interface{}

	//for id matching
	Match(Id) bool
	MatchType() MatchType

	//Generators for creating other ids of same type. Since often we don't
	//know the exact types of Id.Val, so we have to create new ones from an existing id
	Clone(...int) (Id, error)      //create a new id with same id, but possible diff scope & membership
	SysID(int, ...int) (Id, error) //generate sys ids, also called as method of Router
	SysIdIndex() int               //return (0 - NumSysInternalIds) for SysIds, return -1 for others

	//Stringer interface
	String() string
}

//Indices for sys msgs, used for creating SysIds
const (
	ConnId    = iota //msgs for router connection
	DisconnId        //msgs for router disconnection
	ErrorId          //msgs sent when one side detect errors
	ReadyId          //msgs sent when router's chans ready to recv more msgs
	PubId            //send new publications (set<id, chan type info>)
	UnPubId          //remove publications from connected routers
	SubId            //send new subscriptions (set<id, chan type info>)
	UnSubId          //remove subscriptions from connected routers
	NumSysIds
)

//Some system level internal ids (for router internal logging and fault reporting)
const (
	RouterLogId = NumSysIds + iota
	RouterFaultId
	NumSysInternalIds
)

var sysIdxString []string = []string {"ConnId", "DisconnId", "ErrorId", "ReadyId", "PubId", "UnPubId", "SubId", "UnSubId", "RouterLogId", "RouterFaultId"}

//A function used as predicate in router.idsForSend()/idsForRecv() to find all ids in a router's
//namespace which are exported to outside
func ExportedId(id Id) bool {
	return id.Member() == MemberLocal && (id.Scope() == ScopeRemote || id.Scope() == ScopeGlobal)
}

//Use integer as ids in router
type IntId struct {
	Val       int
	ScopeVal  int
	MemberVal int
}

func (id IntId) Clone(args ...int) (nnid Id, err error) {
	nid := &IntId{Val: id.Val, ScopeVal: id.ScopeVal, MemberVal: id.MemberVal}
	l := len(args)
	if l > 0 {
		nid.ScopeVal = args[0]
	}
	if l > 1 {
		nid.MemberVal = args[1]
	}
	nnid = nid
	return
}

func (id IntId) Key() interface{} { return id.Val }

func (id1 IntId) Match(id2 Id) bool {
	if id3, ok := id2.(*IntId); ok {
		return id1.Val == id3.Val
	}
	return false
}

func (id IntId) MatchType() MatchType { return ExactMatch }

func (id IntId) Scope() int  { return id.ScopeVal }
func (id IntId) Member() int { return id.MemberVal }
func (id IntId) String() string {
	return fmt.Sprintf("%d_%s_%s", id.Val, scopeString[id.ScopeVal], memberString[id.MemberVal])
}
//define 8 system msg ids
var IntSysIdBase int = -10101 //Base value for SysIds of IntId
func (id IntId) SysID(indx int, args ...int) (ssid Id, err error) {
	if indx < 0 || indx >= NumSysInternalIds {
		err = errors.New(errInvalidSysId)
		return
	}
	sid := &IntId{Val: (IntSysIdBase - indx)}
	l := len(args)
	if l > 0 {
		sid.ScopeVal = args[0]
	}
	if l > 1 {
		sid.MemberVal = args[1]
	}
	ssid = sid
	return
}
func (id IntId) SysIdIndex() int {
	idx := IntSysIdBase - id.Val
	if idx >= 0 && idx < NumSysInternalIds {
		return idx
	}
	return -1
}

//Use strings as ids in router
type StrId struct {
	Val       string
	ScopeVal  int
	MemberVal int
}

func (id StrId) Clone(args ...int) (nnid Id, err error) {
	nid := &StrId{Val: id.Val, ScopeVal: id.ScopeVal, MemberVal: id.MemberVal}
	l := len(args)
	if l > 0 {
		nid.ScopeVal = args[0]
	}
	if l > 1 {
		nid.MemberVal = args[1]
	}
	nnid = nid
	return
}

func (id StrId) Key() interface{} { return id.Val }

func (id1 StrId) Match(id2 Id) bool {
	if id3, ok := id2.(*StrId); ok {
		return id1.Val == id3.Val
	}
	return false
}

func (id StrId) MatchType() MatchType { return ExactMatch }

func (id StrId) Scope() int  { return id.ScopeVal }
func (id StrId) Member() int { return id.MemberVal }
func (id StrId) String() string {
	return fmt.Sprintf("%s_%s_%s", id.Val, scopeString[id.ScopeVal], memberString[id.MemberVal])
}
//define 8 system msg ids
var StrSysIdBase string = "-10101" //Base value for SysIds of StrId
func (id StrId) SysID(indx int, args ...int) (ssid Id, err error) {
	if indx < 0 || indx >= NumSysInternalIds {
		err = errors.New(errInvalidSysId)
		return
	}
	sid := &StrId{Val: (StrSysIdBase + strconv.Itoa(indx))}
	l := len(args)
	if l > 0 {
		sid.ScopeVal = args[0]
	}
	if l > 1 {
		sid.MemberVal = args[1]
	}
	ssid = sid
	return
}
func (id StrId) SysIdIndex() int {
	if len(id.Val) >= 7 && StrSysIdBase == id.Val[0:6] {
		idx := int(id.Val[6] - '0') //strconv.Atoi(id.Val[6:])
		if idx >= 0 && idx < NumSysInternalIds {
			return idx
		}
	}
	return -1
}

//Use file-system like pathname as ids
//PathId has diff Match() algo from StrId
type PathId struct {
	Val       string
	ScopeVal  int
	MemberVal int
}

func (id PathId) Clone(args ...int) (nnid Id, err error) {
	nid := &PathId{Val: id.Val, ScopeVal: id.ScopeVal, MemberVal: id.MemberVal}
	l := len(args)
	if l > 0 {
		nid.ScopeVal = args[0]
	}
	if l > 1 {
		nid.MemberVal = args[1]
	}
	nnid = nid
	return
}

func (id PathId) Key() interface{} { return id.Val }

func (id1 PathId) Match(id2 Id) bool {
	if id3, ok := id2.(*PathId); ok {
		p1, p2 := id1.Val, id3.Val
		n1, n2 := len(p1), len(p2)
		//make sure both are valid path names
		if n1 == 0 || p1[0] != '/' {
			return false
		}
		if n2 == 0 || p2[0] != '/' {
			return false
		}
		//check wildcards
		w1, w2 := p1[n1-1] == '*', p2[n2-1] == '*'
		if w1 {
			n1--
		}
		if w2 {
			n2--
		}
		if (n1 < n2) && w1 {
			return p1[0:n1] == p2[0:n1]
		}
		if (n2 < n1) && w2 {
			return p1[0:n2] == p2[0:n2]
		}
		if n1 == n2 {
			return p1[0:n1] == p2[0:n1]
		}
	}
	return false
}

func (id PathId) MatchType() MatchType { return PrefixMatch }

func (id PathId) Scope() int  { return id.ScopeVal }
func (id PathId) Member() int { return id.MemberVal }
func (id PathId) String() string {
	return fmt.Sprintf("%s_%s_%s", id.Val, scopeString[id.ScopeVal], memberString[id.MemberVal])
}
//define 8 system msg ids
var PathSysIdBase string = "/10101" //Base value for SysIds of PathId
func (id PathId) SysID(indx int, args ...int) (ssid Id, err error) {
	if indx < 0 || indx >= NumSysInternalIds {
		err = errors.New(errInvalidSysId)
		return
	}
	sid := &PathId{Val: (PathSysIdBase + "/" + strconv.Itoa(indx))}
	l := len(args)
	if l > 0 {
		sid.ScopeVal = args[0]
	}
	if l > 1 {
		sid.MemberVal = args[1]
	}
	ssid = sid
	return
}
func (id PathId) SysIdIndex() int {
	if len(id.Val) >= 8 && PathSysIdBase == id.Val[0:6] {
		idx := int(id.Val[7] - '0') //strconv.Atoi(id.Val[7:])
		if idx >= 0 && idx < NumSysInternalIds {
			return idx
		}
	}
	return -1
}

//Use a common msgTag as Id
type MsgTag struct {
	Family int //divide all msgs into families: system, fault, provision,...
	Tag    int //further division inside a family
}

func (t MsgTag) String() string { return fmt.Sprintf("%d_%d", t.Family, t.Tag) }

type MsgId struct {
	Val       MsgTag
	ScopeVal  int
	MemberVal int
}

func (id MsgId) Clone(args ...int) (nnid Id, err error) {
	nid := &MsgId{Val: id.Val, ScopeVal: id.ScopeVal, MemberVal: id.MemberVal}
	l := len(args)
	if l > 0 {
		nid.ScopeVal = args[0]
	}
	if l > 1 {
		nid.MemberVal = args[1]
	}
	nnid = nid
	return
}

func (id MsgId) Key() interface{} { return id.Val.String() }

func (id1 MsgId) Match(id2 Id) bool {
	if id3, ok := id2.(*MsgId); ok {
		return id1.Val.Family == id3.Val.Family &&
			id1.Val.Tag == id3.Val.Tag
	}
	return false
}

func (id MsgId) MatchType() MatchType { return ExactMatch }

func (id MsgId) Scope() int  { return id.ScopeVal }
func (id MsgId) Member() int { return id.MemberVal }
func (id MsgId) String() string {
	return fmt.Sprintf("%v_%s_%s", id.Val, scopeString[id.ScopeVal], memberString[id.MemberVal])
}
//define 8 system msg ids
var MsgSysIdBase MsgTag = MsgTag{-10101, -10101} //Base value for SysIds of MsgId
func (id MsgId) SysID(indx int, args ...int) (ssid Id, err error) {
	if indx < 0 || indx >= NumSysInternalIds {
		err = errors.New(errInvalidSysId)
		return
	}
	msgTag := MsgSysIdBase
	msgTag.Tag -= indx
	sid := &MsgId{Val: msgTag}
	l := len(args)
	if l > 0 {
		sid.ScopeVal = args[0]
	}
	if l > 1 {
		sid.MemberVal = args[1]
	}
	ssid = sid
	return
}
func (id MsgId) SysIdIndex() int {
	if id.Val.Family == -10101 {
		idx := -10101 - id.Val.Tag
		if idx >= 0 && idx < NumSysInternalIds {
			return idx
		}
	}
	return -1
}

//Various Id constructors

//Some dummy ids, often used as seedId when creating router
var (
	DummyIntId  Id = &IntId{Val: -10201}
	DummyStrId  Id = &StrId{Val: "-10201"}
	DummyPathId Id = &PathId{Val: "-10201"}
	DummyMsgId  Id = &MsgId{Val: MsgTag{-10201, -10201}}
)

/*
 IntId constructor, accepting the following arguments:
 Val       int
 ScopeVal  int
 MemberVal int
*/
func IntID(args ...interface{}) Id {
	l := len(args)
	if l == 0 {
		return DummyIntId
	}
	id := &IntId{}
	iv, ok := args[0].(int)
	if !ok {
		return nil
	}
	id.Val = iv
	if l > 1 {
		if iv, ok = args[1].(int); ok {
			id.ScopeVal = iv
		} else {
			return nil
		}
	}
	if l > 2 {
		if iv, ok = args[2].(int); ok {
			id.MemberVal = iv
		} else {
			return nil
		}
	}
	return id
}

/*
 StrId constructor, accepting the following arguments:
 Val       string
 ScopeVal  int
 MemberVal int
*/
func StrID(args ...interface{}) Id {
	l := len(args)
	if l == 0 {
		return DummyStrId
	}
	id := &StrId{}
	sv, ok := args[0].(string)
	if !ok {
		return nil
	}
	id.Val = sv
	if len(id.Val) == 0 {
		return nil
	}
	if l > 1 {
		if iv, ok := args[1].(int); ok {
			id.ScopeVal = iv
		} else {
			return nil
		}
	}
	if l > 2 {
		if iv, ok := args[2].(int); ok {
			id.MemberVal = iv
		} else {
			return nil
		}
	}
	return id
}

/*
 PathId constructor, accepting the following arguments:
 Val       string (path names, such as /sport/basketball/news/...)
 ScopeVal  int
 MemberVal int
*/
func PathID(args ...interface{}) Id {
	l := len(args)
	if l == 0 {
		return DummyPathId
	}
	id := &PathId{}
	sv, ok := args[0].(string)
	if !ok {
		return nil
	}
	id.Val = sv
	if len(id.Val) == 0 || id.Val[0] != '/' {
		return nil
	}
	if l > 1 {
		if iv, ok := args[1].(int); ok {
			id.ScopeVal = iv
		} else {
			return nil
		}
	}
	if l > 2 {
		if iv, ok := args[2].(int); ok {
			id.MemberVal = iv
		} else {
			return nil
		}
	}
	return id
}

/*
 MsgId constructor, accepting the following arguments:
 Family    int
 Tag       int
 ScopeVal  int
 MemberVal int
*/
func MsgID(args ...interface{}) Id {
	l := len(args)
	if l == 0 {
		return DummyMsgId
	}
	if l == 1 {
		//should have at least 2 ints for family & tag
		return nil
	}
	id := &MsgId{}
	iv, ok := args[0].(int)
	if !ok {
		return nil
	}
	id.Val.Family = iv
	iv, ok = args[1].(int)
	if !ok {
		return nil
	}
	id.Val.Tag = iv
	if l > 2 {
		if iv, ok = args[2].(int); ok {
			id.ScopeVal = iv
		} else {
			return nil
		}
	}
	if l > 3 {
		if iv, ok = args[3].(int); ok {
			id.MemberVal = iv
		} else {
			return nil
		}
	}
	return id
}

// for scope checking
var scope_check_table [][]int = [][]int{
	[]int{1, 0, 1, 0, 0, 1},
	[]int{0, 0, 0, 0, 0, 1},
	[]int{1, 0, 1, 0, 0, 0},
	[]int{0, 0, 0, 0, 0, 0},
	[]int{0, 0, 0, 0, 0, 0},
	[]int{1, 1, 0, 0, 0, 0},
}

//validate the src and dest scope matches
func scope_match(src, dst Id) bool {
	src_row := src.Member()*NumScope + src.Scope()
	dst_col := dst.Member()*NumScope + dst.Scope()
	return scope_check_table[src_row][dst_col] == 1
}
