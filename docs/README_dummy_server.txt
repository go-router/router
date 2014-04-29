        << a dummy server tries to be reliable - in the way of telecom switch >>

1. Background

In the past, i had the luck to work in telecom industry developing switch control software. A telecom switch is a distributed system of relative size: a backbone switch can have 2-3 frames, each frame contains 8 or 16 shelves, each shelf contains 16 or 32 slots, into each slot we can insert a card/board, the card/board contains circuits for application logic: io ports, switch fabric, or control, etc.. normally a card/board has a CPU, switch/shelf controllers use two boards/CPUs, one active and one standby. 

In the various projects i worked with, there were two kinds of software designs. One uses layers of OO frameworks and design patterns and using Corba for inter-controller communications. The resulted systems are complicated. The other uses plain old C and light weight message passing, and a few simple rules as described below. The resulted system is relatively simple and scalable.

The simple rules for message passing based design are as following:
1> partition application functionalities into distinct tasks (threads or processes)
2> define the interactions between tasks as messages (message ids and message data structs). in other words, define the public interfaces of tasks as the set of messages they will send and the set of messages they will recv
3> one task can only interact with another task by sending messages to it. a task's state is private (normally one part of application state) which no other tasks can change directly without going thru the messaging interface.

In my understanding, the above rules are simply another interpretation of the principle Rob and other Go developers have advocated:
   "Do not communicate by sharing memory; instead, share memory by communicating."

2. Design

As an exercise, we'll try to implement a dummy server following the above message based design. And use a goroutine for each application task.

normally, we'll partition the id space into sections, each of which is for an application functionality, handled by one or a group of tasks. for example, if we use integer as ids, we can divide id space as following:
          . 101-200 for system management
          . 201-300 for performance management
          . 301-400 for fault management
          . ...

1> System partition

For our dummy server, i'll use path names as ids and partition the systems as following:

<1> SystemManager Task
    To make our server reliable, let's have two server instances running at the same time, one active (serving the requests), one standby (ready to replace the active one)
    SystemManager at standby server monitors the heartbeats from active server, if 2 heartbeats miss in a row, standby will become active and start serving user requests
    SystemManager at active server will send heartbeats to standby, and send commands to other tasks to drive their life cycle.
   A> messages sent
      /Sys/Ctrl/Heartbeat
          active servant sends heartbeats to standby servant
      /Sys/Command
          commands sent to control subordinate tasks' life cycle: Start, Stop, Shutdown
          commands sent to manage app services: AddService, DelService
   B> messages recved
      /Sys/Ctrl/Heartbeat
          standby servant monitors heartbeats from active servant
      /Sys/OutOfService
          fault manager will send OOS msg to system mananger when system has fault and become unusable
      SysId(PubId/UnPubId) 
         system manager will subscribe to these two system ids to detect the join/leave of clients.
         to simplify our sample, when client connect and subscribe to a service, sysetm manager will grab the service name from the subscription and start a service with that name. so the server will start with no app services.
         in real world, the server probably starts with a set of app services.

<2> Service Task 
    a dummy application service task, just bouncing back request id with some transaction number.
    should be one task per service, differentiated by unique "ServiceName"
    a real server could provide multiple services, thus have mulit ServiceTasks running
   A> messages sent
      /App/ServiceName/Response
          send response to client's request received at "/App/ServiceName/Request"
      /Fault/AppService/Exception
          randomly send fake app exception to fault manager
      /DB/Request
          randomly send fake DB requests to DbTask
   B> messages recved
      /App/ServiceName/Request
          receive client requests
      /App/ServiceName/Command
          system tasks can send commands to control service task: Start, Stop, Reset.
          currently only fault manager will send Reset when it receives an exception from this service task
      /App/ServiceName/DB/Response
          receive response from DbTask for DB requests sent
      /Sys/Command
          receive commands from system manager, mostly for life cycle management, such as Start, Stop

<3> Database Task
    a database task manages database connections, carries out db transactions on behalf of service tasks. currently it does nothing besides sending back empty db responses and randomly generate exception.
   A> messages sent
      /App/ServiceName/DB/Response
          send db transaction result back to service task with name "ServiceName"
      /Fault/DB/Exception
          randomly raise db exception to fault manager
   B> messages recved
      /DB/Request
          receive db requests from service tasks
      /Sys/Command
          receive commands from system manager, mostly for life cycle management

<4> FaultManager Task
    A fault manager will receive FaultRecords from other tasks, perform fault correlation and handling. Normally for reasonably sized system, we can have a hierachy of fault managers. Low level fault managers will handle local fault for a specific functionality or app module, and propagate the faults to upper level managers if it cannot be handled locally.
   A> messages sent
      /Sys/OutOfService
          send msg to system manager to notify that system becomes unusable because of some faults (for our simple sample, a fault from DbTask will do this)
      /App/ServiceName/Command
          for our simple sample, fault manager sends "Reset" to service task when receiving fault from it
   B> messages recved
      /Fault/DB/Exception
            receive db exception
      /Fault/AppService/Exception
            receive app service exception
      /Sys/Command
           receive commands from system manager, mostly for life cycle management

2> Task

Each task is a running goroutine which will go thru the following standard stages of life cycle:
     init
     start
     stop
     shutdown

The following are the jobs performed at each stage:
init
    create channels and attach channels to ids in router
    possible other jobs:
       load config data
       open conn to database
       open conn to backend legacy server
start
    actively perform service, handle user requests, send responses
stop
    pause active service
shutdown
    detach chans from router
    close conn to database
    close conn to other servers

This may look similar to the life cycle of Java's applet. However they are really different:
    A> Java applets' life cycle methods are called by browser/JVM at proper moments. they expose call-back interface to runtime framework. 
    B> A task is active with its own goroutine. A task's life cycle is driven by messages from SystemManager Task. A task's public interface are solely the set of messages it sends and the set of messages it receives. A task exposes no public methods or public data. 

3> Program structure
   
   main() function is simple:
   . create routers and connect them as need
   . create Tasks and connect them to routers

   To simplify our code, the dummy server code is organized as following:
   . create a "Servant" struct which contains a router listening at some socket addr/port for incoming client connections
   . inside a Servant, create instances of above tasks attached to the router. in real world, for load balance or reliability, we could configure the tasks of a Servant running distributedly with two routers on two machines. these two routers can be connected thru sockets and tasks at each machine connected to its local router. Tasks' code do not need change in new configuration.
   . for simplicity, in main() function of dummy server, we create two instances of Servant, one active and the other standby. in real world, we may deploy these two instances of servant as two processes, or two machines for more reliability.
   . connect the routers of these two servants with filters defined at proxies to allow only heartbeat messages passed between them
   . when clients connect to dummy server, it will connect to both servant instances, although at any moment, only the active servant instance is providing the service and answering client requests

3. code
   code is under samples/dummyserver.
   tasks: sysmgrtask.go svctask.go dbtask.go faultmgrtask.go
   server: servant.go server.go
   client: client.go

4. How to run it

1> in one console, run "./server" to start the server
2> in 2nd console, run "./client news 1234" to start a client to talk to "news" service in server for 1234 times
3> in 3rd console, run "./client stock 1234" to start a client to talk to "stock" service in server for 1234 times
4> observe the trace messages in server console, see how the standby servant will come up automatically when active servant goes out of service:
"...
App Service [ news ] at [ servant1 ] process req:  request 1616
App Service [ stock ] at [ servant1 ] process req:  request 1822
App Service [ stock ] at [ servant1 ] process req:  request 1823
DbTask at [ servant1 ] handles req from :  stock
DbTask at [ servant1 ] report fault
fault manager at [ servant1 ] report OOS
xxxx Servant [ servant1 ] will take a break and standby ...
App Service [ stock ] at [ servant1 ] is stopped
servant1  enter monitor heartbeat
App Service [ news ] at [ servant1 ] is stopped
servant1  exit send heartbeat
servant2  exit monitor heartbeat
!!!! Servant [ servant2 ] come up in service ...
servant2  enter send heartbeat
App Service [ news ] at [ servant2 ] is activated
App Service [ stock ] at [ servant2 ] is activated
App Service [ stock ] at [ servant2 ] process req:  request 1824
App Service [ stock ] at [ servant2 ] process req:  request 1825
...
"
5> observe the trace messages in client console, see when servant fail-over/switch-over happens, the client may have one request timed out, then the responses will keep coming back, however from a different servant:
"...
client sent request [request 1822] to serivce [stock]
client recv response ( [request 1822] is processed at [servant1] : transaction_id [4] )
client sent request [request 1823] to serivce [stock]
client recv response ( [request 1823] is processed at [servant1] : transaction_id [5] )
client sent request [request 1824] to serivce [stock]
time out for reqest [request 1824]
client sent request [request 1824] to serivce [stock]
client recv response ( [request 1824] is processed at [servant2] : transaction_id [4] )
client sent request [request 1825] to serivce [stock]
client recv response ( [request 1825] is processed at [servant2] : transaction_id [5] )
...
"

