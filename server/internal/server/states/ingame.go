package states

import (
	"context"
	"fmt"
	"log"
	"math"
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
	game.player.X, game.player.Y = objects.SpawnCoords(game.player.Radius, game.client.SharedGameObjects().Players, nil)
	game.player.Speed = 15.0
	game.player.Radius = 20

	// Send the player's initial state to the client
	game.client.SocketSend(packets.NewPlayer(game.client.Id(), game.player))

	// Send the spores to the client in the background
	go game.sendInitialSpores(80, 25 * time.Millisecond)
}

func (game *InGame) HandleMessage(senderId uint64, message packets.Msg) {
	switch message := message.(type) {
		case *packets.Packet_Player:
			game.handlePlayer(senderId, message)
		case *packets.Packet_PlayerDirection:
			game.handlePlayerDirection(senderId, message)
		case *packets.Packet_Chat:
			game.handleChat(senderId, message)
		case *packets.Packet_SporeConsumed:
			game.handleSporeConsumed(senderId, message)
		case *packets.Packet_PlayerConsumed:
			game.handlePlayerConsumed(senderId, message)
		case *packets.Packet_Spore:
			game.handleSpore(senderId, message)
	}
}

func (game *InGame) OnExit() {
	if game.cancelPlayerUpdateLoop != nil {
		game.cancelPlayerUpdateLoop()
	}

	game.client.SharedGameObjects().Players.Remove(game.client.Id())
}

func (game *InGame) sendInitialSpores(batchSize int, delay time.Duration) {
	sporesBatch := make(map[uint64]*objects.Spore, batchSize)

	game.client.SharedGameObjects().Spores.ForEach(func(sporeId uint64, spore *objects.Spore) {
		sporesBatch[sporeId] = spore

		if len(sporesBatch) >= batchSize {
			game.client.SocketSend(packets.NewSporesBatch(sporesBatch))
			sporesBatch = make(map[uint64]*objects.Spore, batchSize)
			time.Sleep(delay)
		}
	})

	// Send any remaining spores
	if len(sporesBatch) > 0 {
		game.client.SocketSend(packets.NewSporesBatch((sporesBatch)))
	}
}

func (game *InGame) handlePlayer(senderId uint64, message *packets.Packet_Player) {
	if senderId == game.client.Id() {
		game.logger.Println("Received player message from our own client, ignoring")
		return
	}

	game.client.SocketSendAs(message, senderId)
}

func (game *InGame) handleSpore(senderId uint64, message *packets.Packet_Spore) {
	game.client.SocketSendAs(message, senderId)
}

func (game *InGame) handleChat(senderId uint64, message *packets.Packet_Chat) {
	if senderId == game.client.Id() {
		game.client.Broadcast(message)
	} else {
		game.client.SocketSendAs(message, senderId)
	}
}

func (game *InGame) handleSporeConsumed(senderId uint64, message *packets.Packet_SporeConsumed) {
	if senderId != game.client.Id() {
		game.client.SocketSendAs(message, senderId)
		return
	}

	// If the spore was supposely consumed by our player
	errorMessage := "Could not verify spore consumption: "

	// Check if spore exists
	sporeId := message.SporeConsumed.SporeId
	spore, err := game.getSpore(sporeId)

	if err != nil {
		game.logger.Println(errorMessage + err.Error())
		return
	}

	// Check if the spore is close enough to be consumed
	err = game.validatePlayerCloseToObject(spore.X, spore.Y, spore.Radius, 10)

	if err != nil {
		game.logger.Println(errorMessage + err.Error())
		return
	}

	// The spore consumption is valid, so grow the player and remove the spore
	sporeMass := radiusToMass(spore.Radius)
	newRadius := game.nextRadius(sporeMass)
	game.player.Radius = newRadius

	go game.client.SharedGameObjects().Spores.Remove(senderId)

	message.SporeConsumed.NewRadius = newRadius

	game.client.Broadcast(message)
}

func (game *InGame) handlePlayerConsumed(senderId uint64, message *packets.Packet_PlayerConsumed) {
	if senderId != game.client.Id() {
		game.client.SocketSendAs(message, senderId)

		if message.PlayerConsumed.PlayerId == game.client.Id() {
			game.logger.Println("Player was consumed, respawning")
			game.client.SetState(&InGame{
				player: &objects.Player{
					Name: game.player.Name,
				},
			})
		}

		return
	}

	errorMessage := "Could not verify player consumption: "

	otherId := message.PlayerConsumed.PlayerId	
	other, err := game.getOtherPlayer(otherId)

	if err != nil {
		game.logger.Println(errorMessage, err.Error())
		return
	}

	ourMass := radiusToMass(game.player.Radius)
	otherMass := radiusToMass(other.Radius)
	
	if ourMass <= otherMass*1.5 {
		game.logger.Printf(errorMessage + "player not massive enough to consume the other player (our radius: %f, other radius: %f)", game.player.Radius, other.Radius)
		return
	}

	err = game.validatePlayerCloseToObject(other.X, other.Y, other.Radius, 10)

	if err != nil {
		game.logger.Println(errorMessage, err.Error())
		return
	}

	newRadius := game.nextRadius(otherMass)
	game.player.Radius = newRadius
	
	go game.client.SharedGameObjects().Players.Remove(otherId)

	message.PlayerConsumed.NewRadius = newRadius

	game.client.Broadcast(message)
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

func (game *InGame) getSpore(sporeId uint64) (*objects.Spore, error) {
	spore, exists := game.client.SharedGameObjects().Spores.Get(sporeId)

	if !exists {
		return nil, fmt.Errorf("Spore with ID %d does not exist", sporeId)
	}

	return spore, nil
}

func (game *InGame) getOtherPlayer(playerId uint64) (*objects.Player, error) {
	player, exists := game.client.SharedGameObjects().Players.Get(playerId)

	if !exists {
		return nil, fmt.Errorf("Player with ID %d does not exist", playerId)
	}

	return player, nil
}

func (game *InGame) validatePlayerCloseToObject(objX, objY, objRadius, buffer float64) error {
	realDX := game.player.X - objX
	realDY := game.player.Y - objY
	realDistSq := realDX * realDX + realDY * realDY
	thresholdDist := game.player.Radius + buffer + objRadius
	thresholdDistSq := thresholdDist * thresholdDist

	if realDistSq > thresholdDistSq {
		return fmt.Errorf("Player is too far from the object (distSq: %f, thresholdSq: %f)", realDistSq, thresholdDistSq)
	}

	return nil
}

func radiusToMass(radius float64) float64 {
	return math.Pi * radius * radius
}

func massToRadius(mass float64) float64 {
	return math.Sqrt(mass / math.Pi)
}

func (game *InGame) nextRadius(massDiff float64) float64 {
	oldMass := radiusToMass(game.player.Radius)
	newMass := oldMass + massDiff

	return massToRadius(newMass)
}