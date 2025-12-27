package server

import (
	"context"
	"database/sql"
	_ "embed"
	"log"
	"math/rand"
	"net/http"
	"server/internal/server/db"
	"server/internal/server/objects"
	"server/pkg/packets"
	"time"

	_ "modernc.org/sqlite"
)

const MaxSpores = 1000

//go:embed db/config/schema.sql
var schemaGenSql string

type DbTransaction struct {
	Ctx context.Context
	Queries *db.Queries
}

type SharedGameObjects struct {
	// The ID of the player is the ID of the client
	Players *objects.SharedCollection[*objects.Player]
	Spores *objects.SharedCollection[*objects.Spore]
}

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

	// A reference to the database transaction context for this client
	DbTransaction() *DbTransaction

	SharedGameObjects() *SharedGameObjects

	// Close the client's connections and cleanup
	Close(reason string)
}

type Hub struct {
	Clients *objects.SharedCollection[ClientInterfacer]

	// Packets in this channel will be processed by all connected clients except the sender
	BroadcastChan chan *packets.Packet

	// Clients in this channel will be registered to the hub
	RegisterChan chan ClientInterfacer

	// Clients in this channel will be unregistered from the hub
	UnregisterChan chan ClientInterfacer

	// Database connection pool
	dbPool *sql.DB

	SharedGameObjects *SharedGameObjects 
}

func (hub *Hub) NewDbTransaction() *DbTransaction {
	return &DbTransaction{
		Ctx: context.Background(),
		Queries: db.New(hub.dbPool),
	}
}

func NewHub() *Hub {
	dbPool, err:= sql.Open("sqlite", "db.sqlite")

	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}

	return &Hub{
		Clients: objects.NewSharedCollection[ClientInterfacer](),
		BroadcastChan: make(chan *packets.Packet),
		RegisterChan: make(chan ClientInterfacer),
		UnregisterChan: make(chan ClientInterfacer),
		SharedGameObjects: &SharedGameObjects{
			Players: objects.NewSharedCollection[*objects.Player](),
			Spores: objects.NewSharedCollection[*objects.Spore](),
		},
		dbPool: dbPool,
	}
}

func (hub *Hub) Run() {
	log.Println("Initializing database...")

	if _, err := hub.dbPool.ExecContext(context.Background(), schemaGenSql); err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}

	log.Println("Placing spores...")
	for i := 0; i < MaxSpores; i++ {
		hub.SharedGameObjects.Spores.Add(hub.newSpore())
	}

	go hub.replenishSporesLoop(2 * time.Second)

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

func (hub *Hub) newSpore() *objects.Spore {
	sporeRadius := max(10 + rand.NormFloat64() * 3, 5)
	x, y := objects.SpawnCoords(sporeRadius, hub.SharedGameObjects.Players, hub.SharedGameObjects.Spores)

	return &objects.Spore{X: x, Y: y, Radius: sporeRadius}
}

func (hub *Hub) replenishSporesLoop(rate time.Duration) {
	ticker := time.NewTicker(rate)
	defer ticker.Stop()

	for range ticker.C {
		sporesRemaining := hub.SharedGameObjects.Spores.Len()
		diff := MaxSpores - sporesRemaining

		if diff <= 0 {
			continue
		}

		log.Printf("%d spores remain - going to replenish %d spores", sporesRemaining, diff)

		// Don't really want to spawn too many at a time, otherwise it can cause lag spikes
		for i := 0; i < min(diff, 10); i++ {
			spore := hub.newSpore()
			sporeId := hub.SharedGameObjects.Spores.Add(spore)

			hub.BroadcastChan <- &packets.Packet{
				SenderId: 0,
				Msg: packets.NewSpore(sporeId, spore),
			}

			// Sleep a little bit to avoid lag spikes
			time.Sleep(50 * time.Millisecond)
		}
	}
}