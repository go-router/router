Let's do some ping-pong...

First, there is a Pinger and a Ponger and they bounce a string msg 
between them as the ball. They are "active" players, each has
a goroutine (Run() method), so let's follow principle and 
design their public interface as channels: Ponger will send 
the msg ball to pong_chan and recv the msg ball 
from ping_chan, while Pinger will reverse the 
direction. A Pinger or Ponger has definition similar to following:
type Ponger struct {
	//Ponger's public interface
	pongChan chan<-string
	pingChan <-chan string
	//Ponger's private state
	count int
}

To set up a ping-pong game between Pinger and Ponger, we need to set up
proper ping_chan and pong_chan before starting Pinger/Ponger's 
goroutine bodies in constructor newPinger()/newPonger().

In the first game (game1), we create channels pingChan and pongChan in main()
and pass proper channel ends to newPinger() and newPonger() to set up conn
between Pinger/Ponger.

In the 2nd game (game2), we set the players a step apart. Instead of 
direct channel connection, we create a router in main(), pass it to newPinger()
and newPonger(), inside which new pingChan/pongChan are created, attached
to proper ids "ping" or "pong" in router, so the connection between Pinger
and Ponger is set up thru router.

In 3rd game (game3), we set the players two steps apart. Instead of 
connecting Pinger/Ponger thru the same router, we create two routers, one for Pinger
and the other for Ponger. These two routers are connected directly and Pinger and
Ponger are attached to one of them.

In 4th game (game4), we set the players further apart. Instead of 
two routers directly connected, we create a unix sock connection,
create and connect a router at both ends of it, and attach pingChan/pongChan to one of
the routers.

In the above three configurations, Pinger and Ponger's interface and code remain
the same, the only thing changed is the way we set up channel connection between them.
As long as their interacting channels (pingChan & pongChan) are properly set up,
Pinger and Ponger can function correctly, no matter they are connected to the same
router in same address space, or connected thru sockets running in separate processes.


