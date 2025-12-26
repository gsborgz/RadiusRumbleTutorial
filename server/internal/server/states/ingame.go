package states

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"server/internal/server"
	"server/internal/server/objects"
	"server/pkg/packets"
	"time"
)

type InGame struct {
	client server.ClientInterfacer
	player *objects.Player
	logger *log.Logger
	cancelPlayerUpdateLoop context.CancelFunc
}

func (game *InGame) Name() string {
	return "InGame"
}

func (game *InGame) SetClient(client server.ClientInterfacer) {
	game.client = client
	loggingPrefix := fmt.Sprintf("Client %d [%s]: ", client.Id(), game.Name())
	game.logger = log.New(log.Writer(), loggingPrefix, log.LstdFlags)
}

func (game *InGame) OnEnter() {
	game.logger.Printf("Adding player %s to the shared collection", game.player.Name)
	go game.client.SharedGameObjects().Players.Add(game.player, game.client.Id())

	// Set the initial properties of the player
	game.player.X = rand.Float64() * 1000
	game.player.Y = rand.Float64() * 1000
	game.player.Speed = 15.0
	game.player.Radius = 20

	// Send the player's initial state to the client
	game.logger.Println("olá")
	game.client.SocketSend(packets.NewPlayer(game.client.Id(), game.player))
}

func (game *InGame) HandleMessage(senderId uint64, message packets.Msg) {
	switch message := message.(type) {
		case *packets.Packet_Player:
			game.handlePlayer(senderId, message)
		case *packets.Packet_PlayerDirection:
			game.handlePlayerDirection(senderId, message)
	}
}

func (game *InGame) OnExit() {
	if game.cancelPlayerUpdateLoop != nil {
		game.cancelPlayerUpdateLoop()
	}

	game.client.SharedGameObjects().Players.Remove(game.client.Id())
}

func (game *InGame) handlePlayer(senderId uint64, message packets.Msg) {
	if senderId == game.client.Id() {
		return
	}

	game.client.SocketSendAs(message, senderId)
}

func (game *InGame) handlePlayerDirection(senderId uint64, message *packets.Packet_PlayerDirection) {
	if senderId == game.client.Id() {
		game.player.Direction = message.PlayerDirection.Direction

		// If this is the first time receiving a direction from the client, start the player update loop
		if game.cancelPlayerUpdateLoop == nil {
			ctx, cancel := context.WithCancel(context.Background())

			game.cancelPlayerUpdateLoop = cancel

			go game.updatePlayerLoop(ctx)
		}
	}
}


func (game *InGame) updatePlayerLoop(ctx context.Context) {
	const delta float64 = 0.05

	ticker := time.NewTicker(time.Duration(delta * 100) * time.Millisecond)

	defer ticker.Stop()

	for {
		select {
			case <- ticker.C:
				game.syncPlayer(delta)
			case <- ctx.Done():
				return
		}
	}
}

func (game *InGame) syncPlayer(delta float64) {
	newX := game.player.X + game.player.Speed * math.Cos(game.player.Direction) * delta
	newY := game.player.Y + game.player.Speed * math.Sin(game.player.Direction) * delta

	game.player.X = newX
	game.player.Y = newY

	updatePacket := packets.NewPlayer(game.client.Id(), game.player)

	game.client.Broadcast(updatePacket)

	go game.client.SocketSend(updatePacket)
}