//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"log"
	"reflect"
	"sync"
	"time"
)

//Some basic router internal error msgs for log and fault
var (
	errIdTypeMismatch        = "id type mismatch"
	errRouterTypeMismatch    = "router type mismatch"
	errInvalidChan           = "invalid channels be attached to router; valid channel types: chan bool/int/float/string/*struct"
	errInvalidBindChan       = "invalid channels for notifying sender/recver attachment"
	errChanTypeMismatch      = "channels of diff type attach to matched id"
	errChanGenericType       = "channels of gnericMsg attached as first"
	errDetachChanNotInRouter = "try to detach a channel which is not attached to router"
	errDupAttachment         = "a channel has been attached to same id more than once"
	errInvalidId             = "invalid id"
	errInvalidSysId          = "invalid index for System Id"

	errConnFail            = "remote conn failed, possibly router type mismatch"
	errConnInvalidMsg      = "remote conn failed, invalid msg transaction"
	errRmtIdTypeMismatch   = "remote conn failed, remote router id type mismatch"
	errRmtChanTypeMismatch = "remote conn failed, remote chan type mismatch"
	//...more
)

type LogPriority int

const (
	LOG_INFO LogPriority = iota
	LOG_DEBUG
	LOG_WARN
	LOG_ERROR
)

func (lp LogPriority) String() string {
	switch lp {
	case LOG_INFO:
		return "INFO"
	case LOG_DEBUG:
		return "DEBUG"
	case LOG_WARN:
		return "WARNING"
	case LOG_ERROR:
		return "ERROR"
	}
	return "InvalidLogType"
}

//LogRecord stores the log information
type LogRecord struct {
	Pri       LogPriority
	Source    string
	Info      interface{}
	Timestamp int64
}

//a LogRecord sender
type logger struct {
	routCh  *RoutedChan
	asyncCh Channel
	source  string
	logChan chan *LogRecord
	router  Router
	id      Id
}

func newlogger(id Id, r Router, src string, bufSize int) *logger {
	logger := new(logger)
	logger.id = id
	logger.router = r
	logger.source = src
	logger.logChan = make(chan *LogRecord, bufSize)
	var err error
	logger.routCh, err = logger.router.AttachSendChan(id, logger.logChan)
	//use async chan to send logs so that logger is nonblocking
	logger.asyncCh = &asyncChan{Channel: logger.routCh}
	if err != nil {
		log.Panicln("failed to add logger for ", logger.source)
		return nil
	}
	return logger
}

func (l *logger) log(p LogPriority, msg interface{}) {
	if l.routCh.NumPeers() == 0 {
		return
	}

	lr := &LogRecord{p, l.source, msg, time.Now().UnixNano()}
	//log all log msgs asynchronously so that it will not block the sender
	l.asyncCh.Send(reflect.ValueOf(lr))
}

func (l *logger) Close() {
	//l.router.DetachChan(l.id, l.logChan)
	l.asyncCh.Close()
}

//Logger can be embedded into user structs / types, which then can use Log() / LogError() directly
type Logger struct {
	router *routerImpl
	logger *logger
	sync.Mutex
}

//NewLogger will create a Logger object which sends log messages thru id in router "r"
func NewLogger(id Id, r Router, src string) *Logger {
	return new(Logger).Init(id, r, src)
}

func (l *Logger) Init(id Id, r Router, src string) *Logger {
	l.Lock()
	defer l.Unlock()
	l.router = r.(*routerImpl)
	if len(src) > 0 {
		l.logger = newlogger(id, l.router, src, DefLogBufSize)
	}
	return l
}

func (l *Logger) Close() {
	l.Lock()
	defer l.Unlock()
	if l.logger != nil {
		ll := l.logger
		l.logger = nil
		ll.Close()
	}
}

//send a log record to log id in router
func (l *Logger) Log(p LogPriority, msg interface{}) {
	l.Lock()
	defer l.Unlock()
	if l.logger != nil {
		l.logger.log(p, msg)
	}
}

//send a log record and store error info in it
func (l *Logger) LogError(err error) {
	l.Lock()
	defer l.Unlock()
	if l.logger != nil {
		l.logger.log(LOG_ERROR, err)
	}
}

//A simple log sink, showing log messages in console.
type LogSink struct {
	sinkChan chan *LogRecord
	sinkExit chan bool
	id       Id
	r        Router
}

//create a new log sink, which receives log messages from id in router "r"
func NewLogSink(id Id, r Router) *LogSink { return new(LogSink).Init(id, r) }

func (l *LogSink) Init(id Id, r Router) *LogSink {
	l.sinkExit = make(chan bool)
	l.sinkChan = make(chan *LogRecord, DefLogBufSize)
	l.id = id
	l.r = r
	l.runConsoleLogSink()
	return l
}

func (l *LogSink) Close() {
	if l.sinkChan != nil {
		l.r.DetachChan(l.id, l.sinkChan)
		//wait for sink gorutine to exit
		<-l.sinkExit
	}
}

func (l *LogSink) runConsoleLogSink() {
	_, err := l.r.AttachRecvChan(l.id, l.sinkChan)
	if err != nil {
		log.Println("*** failed to enable router's console log sink ***")
		return
	}
	go func() {
		for {
			lr, snkOpen := <-l.sinkChan
			if !snkOpen {
				break
			}
			switch lr.Pri {
			case LOG_ERROR:
				fallthrough
			case LOG_WARN:
				//log.Printf("[%s %v %v] %v", lr.Source, lr.Pri, ts, lr.Info);
				log.Printf("[%s %v] %v", lr.Source, lr.Pri, lr.Info)
			case LOG_DEBUG:
				fallthrough
			case LOG_INFO:
				//log.Printf("[%s %v %v] %v", lr.Source, lr.Pri, ts, lr.Info);
				log.Printf("[%s %v] %v", lr.Source, lr.Pri, lr.Info)
			}
		}
		l.sinkExit <- true
		log.Println("console log goroutine exits")
	}()
}

//Fault management

//FaultRecord records some details about fault
type FaultRecord struct {
	Source    string
	Info      error
	Timestamp int64
}

type faultRaiser struct {
	routCh    *RoutedChan
	source    string
	faultChan chan *FaultRecord
	asyncCh   Channel
	router    *routerImpl
	id        Id
	caught    bool
}

func newfaultRaiser(id Id, r Router, src string, bufSize int) *faultRaiser {
	faultRaiser := new(faultRaiser)
	faultRaiser.id = id
	faultRaiser.router = r.(*routerImpl)
	faultRaiser.source = src
	faultRaiser.faultChan = make(chan *FaultRecord, bufSize)
	var err error
	faultRaiser.routCh, err = faultRaiser.router.AttachSendChan(id, faultRaiser.faultChan)
	if err != nil {
		log.Println("failed to add faultRaiser for [%v, %v]", faultRaiser.source, id)
		return nil
	}
	faultRaiser.asyncCh = &asyncChan{Channel: faultRaiser.routCh}
	//log.Stdout("add faultRaiser for ", faultRaiser.source);
	return faultRaiser
}

func (l *faultRaiser) raise(msg error) {
	if l.routCh.NumPeers() == 0 {
		//l.router.Log(LOG_ERROR, fmt.Sprintf("Crash at %v", msg))
		log.Panicf("Crash at %v", msg)
		return
	}

	lr := &FaultRecord{l.source, msg, time.Now().UnixNano()}

	//send all fault msgs async so that caller is not blocked
	l.asyncCh.Send(reflect.ValueOf(lr))
}

func (l *faultRaiser) Close() {
	//l.router.DetachChan(l.id, l.faultChan)
	l.asyncCh.Close()
}

//FaultRaiser can be embedded into user structs/ types, which then can call Raise() directly
type FaultRaiser struct {
	router      *routerImpl
	faultRaiser *faultRaiser
	sync.Mutex
}

//create a new FaultRaiser to send FaultRecords to id in router "r"
func NewFaultRaiser(id Id, r Router, src string) *FaultRaiser {
	return new(FaultRaiser).Init(id, r, src)
}

func (l *FaultRaiser) Init(id Id, r Router, src string) *FaultRaiser {
	l.Lock()
	defer l.Unlock()
	l.router = r.(*routerImpl)
	if len(src) > 0 {
		l.faultRaiser = newfaultRaiser(id, r, src, DefCmdChanBufSize)
	}
	return l
}

func (l *FaultRaiser) Close() {
	l.Lock()
	defer l.Unlock()
	if l.faultRaiser != nil {
		ll := l.faultRaiser
		l.faultRaiser = nil
		ll.Close()
	}
}

//raise a fault - send a FaultRecord to faultId in router
func (r *FaultRaiser) Raise(msg error) {
	r.Lock()
	defer r.Unlock()
	if r.faultRaiser != nil {
		r.faultRaiser.raise(msg)
	} else {
		//r.router.Log(LOG_ERROR, fmt.Sprintf("Crash at %v", msg))
		log.Panicf("Crash at %v", msg)
	}
}
