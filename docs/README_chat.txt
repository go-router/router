a simple chat client/server:

. chatsrv: 
  run "./chatsrv" in a console, will print port number and some status messages
. chatcli:
  run "./chatcli chatter_name srv_name srv_port" in a console
  it will show a simple menu: 1 - Join, 2 - Leave, 3 - Send, 4 - Exit
  Join: 
        will ask for "subject" string, which will be used as Id in router to
        attach a send chan for publishing and attach a recv chan for subscribing
  Leave: 
        unpub/unsub from "subject" id in router
  Send:
        will ask for "subject" and "message" strings which will be sent

. each subject string will become an id in router
. run multi chatcli and all chatcli joined the same subjects can chat with each other

