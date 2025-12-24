package clients

import (
	"fmt"
	"log"
	"net/http"
	"server/internal/server"
	"server/internal/server/states"
	"server/pkg/packets"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

type WebsocketClient struct {
	id   uint64
	conn *websocket.Conn
	hub *server.Hub
	sendChan chan *packets.Packet
	state server.ClientStateHandler
	logger *log.Logger
}

func NewWebsocketClient(hub *server.Hub, writer http.ResponseWriter, request *http.Request) (server.ClientInterfacer, error) {
	upgrader := websocket.Upgrader{
		ReadBufferSize: 1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(writer, request, nil)

	if err != nil {
		return nil, err
	}

	client := &WebsocketClient{
		hub: hub,
		conn: conn,
		sendChan: make(chan *packets.Packet, 256),
		logger: log.New(log.Writer(), "Client unknown: ", log.LstdFlags),
	}

	return client, nil
}

func (client *WebsocketClient) Id() uint64 {
	return client.id;
}

func (client *WebsocketClient) SetState(state server.ClientStateHandler) {
	prevStateName := "None"

	if client.state != nil {
		prevStateName = client.state.Name()
		client.state.OnExit()
	}

	newStateName := "None"

	if state != nil {
		newStateName = state.Name()
	}

	client.logger.Printf("Switching from state %s to %s", prevStateName, newStateName)
	
	client.state = state

	if client.state != nil {
		client.state.SetClient(client)
		client.state.OnEnter()
	}
}

func (client *WebsocketClient) ProcessMessage (senderId uint64, message packets.Msg) {
	client.state.HandleMessage(senderId, message)
}

func (client *WebsocketClient) Initialize(id uint64) {
	client.id = id
	client.logger.SetPrefix(fmt.Sprintf("Client %d: ", client.id))
	client.SetState(&states.Connected{})
}

func (client *WebsocketClient) SocketSend(message packets.Msg) {
	client.SocketSendAs(message, client.id)
}

func (client *WebsocketClient) SocketSendAs(message packets.Msg, senderId uint64) {
	select {
		case client.sendChan <- &packets.Packet{SenderId: senderId, Msg: message}:
		default:
			client.logger.Printf("Send channel full, dropping message: %T", message)
	}
}

func (client *WebsocketClient) PassToPeer(message packets.Msg, peerId uint64) {
	if peer, exists := client.hub.Clients.Get(peerId); exists {
		peer.ProcessMessage(client.id, message)
	}
}

func (client *WebsocketClient) Broadcast(message packets.Msg) {
	client.hub.BroadcastChan <- &packets.Packet{SenderId: client.id, Msg: message}
}

func (client *WebsocketClient) ReadPump() {
	defer func() {
		client.logger.Println("Closing read pump")
		client.Close("Read pump closed")
	}()

	for {
		_, data, err := client.conn.ReadMessage()

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				client.logger.Printf("Error: %v", err)
			}

			break
		}

		packet := &packets.Packet{}
		err = proto.Unmarshal(data, packet)

		if err != nil {
			client.logger.Printf("Error unmarshalling data: %v", err)
			continue
		}

		// To allow the client to lazily not send the sender ID, we'll assume they want to send it to themselves
		if packet.SenderId == 0 {
			packet.SenderId = client.id
		}

		client.ProcessMessage(packet.SenderId, packet.Msg)
	}
}

func (client *WebsocketClient) WritePump() {
	defer func() {
		client.logger.Println("Closing write pump")
		client.Close("Write pump closed")
	}()

	for packet := range client.sendChan {
		writer, err := client.conn.NextWriter(websocket.BinaryMessage)

		if err != nil {
			client.logger.Printf("Error getting writer for %T packet, closing client %v", packet.Msg, err)
			return
		}

		data, err := proto.Marshal(packet)

		if err != nil {
			client.logger.Printf("Error marshalling %T packet, closing client %v", packet.Msg, err)
			continue
		}

		_, err = writer.Write(data)

		if err != nil {
			client.logger.Printf("Error writing %T packet: %v", packet.Msg, err)
			continue
		}

		writer.Write([]byte{'\n'})

		if err = writer.Close(); err != nil {
			client.logger.Printf("Error closing writer for %T packet: %v", packet.Msg, err)
			continue
		}
	}
}

func (client *WebsocketClient) Close(reason string) {
	client.logger.Printf("Closing client connection because: %s", reason)

	client.SetState(nil)

	client.hub.UnregisterChan <- client
	client.conn.Close()

	if _, closed := <-client.sendChan; !closed {
		close(client.sendChan)
	}
}
