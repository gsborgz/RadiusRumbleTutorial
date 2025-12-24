package states

import (
	"fmt"
	"log"
	"server/internal/server"
	"server/pkg/packets"
)

type Connected struct {
	client server.ClientInterfacer
	logger *log.Logger
}

func (connected *Connected) Name() string {
	return "Connected"
}

func (connected *Connected) SetClient(client server.ClientInterfacer) {
	connected.client = client
	loggingPrefix := fmt.Sprintf("Client %d [%s]: ", client.Id(), connected.Name())
	connected.logger = log.New(log.Writer(), loggingPrefix, log.LstdFlags)
}

func (connected *Connected) OnEnter() {
	connected.client.SocketSend(packets.NewId(connected.client.Id()))
}

func (connected *Connected) HandleMessage(senderId uint64, message packets.Msg) {
if senderId == connected.client.Id() {
		// This message was sent by our own client, so broadcast it to everyone else
		connected.client.Broadcast(message)
	} else {
		// Another client interfacer passed this onto us, or it was broadcast from the hub, so foward it to our own client
		connected.client.SocketSendAs(message, senderId)
	}
}

func (connected *Connected) OnExit() {

}