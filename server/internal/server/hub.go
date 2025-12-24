package server

import (
	"log"
	"net/http"
	"server/internal/server/objects"
	"server/pkg/packets"
)

type ClientStateHandler interface {
	Name() string

	// Inject the client into the state handler
	SetClient(client ClientInterfacer)

	OnEnter()
	HandleMessage(senderId uint64, message packets.Msg)

	// Cleanup the state handler and perform any last actions
	OnExit()
}

type ClientInterfacer interface {
	Id() uint64
	ProcessMessage(senderId uint64, message packets.Msg)
	
	// Sets the client's ID and anything else that needs to be initialized
	Initialize(id uint64)

	SetState(newState ClientStateHandler)

	// Puts data from this client into the write pump
	SocketSend(message packets.Msg)

	// Puts data from another client into the write pump
	SocketSendAs(message packets.Msg, senderId uint64)

	// Foward message to another client for processing
	PassToPeer(message packets.Msg, peerId uint64)

	// Foward message to all other clients for processing
	Broadcast(message packets.Msg)
	
	// Pump data from the connected socket directly to the client
	ReadPump()

	// Pump data from the client directly to the connected socket
	WritePump()

	// Close the client's connections and cleanup
	Close(reason string)
}

type Hub struct {
	Clients *objects.SharedCollection[ClientInterfacer]

	BroadcastChan chan *packets.Packet
	RegisterChan chan ClientInterfacer
	UnregisterChan chan ClientInterfacer
}

func NewHub() *Hub {
	return &Hub{
		Clients: objects.NewSharedCollection[ClientInterfacer](),
		BroadcastChan: make(chan *packets.Packet),
		RegisterChan: make(chan ClientInterfacer),
		UnregisterChan: make(chan ClientInterfacer),
	}
}

func (hub *Hub) Run() {
	log.Println("Awaiting client registrations")

	for {
		select {
			case client := <-hub.RegisterChan:
				client.Initialize(hub.Clients.Add(client))
			case client := <-hub.UnregisterChan:
				hub.Clients.Remove(client.Id())
			case packet := <-hub.BroadcastChan:
				hub.Clients.ForEach(func(clientId uint64, client ClientInterfacer) {
					if clientId != packet.SenderId {
						client.ProcessMessage(packet.SenderId, packet.Msg)
					}
				})
		}
	}
}

func (hub *Hub) Serve(getNewClient func (*Hub, http.ResponseWriter, *http.Request) (ClientInterfacer, error), writer http.ResponseWriter, request *http.Request) {
	log.Println("New client connected from", request.RemoteAddr)

	client, err := getNewClient(hub, writer, request)

	if err != nil {
		log.Printf("Error obtaining client for new connection: %v", err)
		return
	}

	hub.RegisterChan <- client

	go client.WritePump()
	go client.ReadPump()
}