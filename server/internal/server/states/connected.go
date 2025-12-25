package states

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"server/internal/server"
	"server/internal/server/db"
	"server/pkg/packets"
	"strings"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

type Connected struct {
	client server.ClientInterfacer
	logger *log.Logger
	queries *db.Queries
	dbCtx context.Context
}

func (connected *Connected) Name() string {
	return "Connected"
}

func (connected *Connected) SetClient(client server.ClientInterfacer) {
	connected.client = client
	loggingPrefix := fmt.Sprintf("Client %d [%s]: ", client.Id(), connected.Name())
	connected.logger = log.New(log.Writer(), loggingPrefix, log.LstdFlags)
	connected.queries = client.DbTransaction().Queries
	connected.dbCtx = client.DbTransaction().Ctx
}

func (connected *Connected) OnEnter() {
	connected.client.SocketSend(packets.NewId(connected.client.Id()))
}

func (connected *Connected) HandleMessage(senderId uint64, message packets.Msg) {
	switch message := message.(type) {
		case *packets.Packet_LoginRequest:
			connected.handleLoginRequest(senderId, message)
		case *packets.Packet_RegisterRequest:
			connected.handleRegisterRequest(senderId, message)
	}
}

func (connected *Connected) OnExit() {

}

func (connected *Connected) handleLoginRequest(senderId uint64, message *packets.Packet_LoginRequest) {
	if senderId != connected.client.Id() {
		return
	}

	username := message.LoginRequest.Username
	password := message.LoginRequest.Password
	genericFailMessage := packets.NewDenyResponse("Incorrect username or password")

	user, err := connected.queries.GetUserByUsername(connected.dbCtx, strings.ToLower(username))

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	err = godotenv.Load()

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	pepper := os.Getenv("PEPPER")
	passwordWithPepper := password + pepper

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(passwordWithPepper))

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	connected.logger.Printf("User %s logged in successfully!", username)
	connected.client.SocketSend(packets.NewOkResponse())
}

func (connected *Connected) handleRegisterRequest(senderId uint64, message *packets.Packet_RegisterRequest) {
	if senderId != connected.client.Id() {
		return
	}

	username := message.RegisterRequest.Username
	password := message.RegisterRequest.Password
	passwordConfirmation := message.RegisterRequest.PasswordConfirmation

	err := validateUserName(username)

	if err != nil {
		reason := fmt.Sprintf("Invalid username: %v", err)
		connected.client.SocketSend(packets.NewDenyResponse(reason))
		return
	}

	err = validatePassword(password, passwordConfirmation)

	if err != nil {
		reason := fmt.Sprintf("Invalid password: %v", err)
		connected.client.SocketSend(packets.NewDenyResponse(reason))
		return
	}

	if _, err := connected.queries.GetUserByUsername(connected.dbCtx, strings.ToLower(username)); err == nil {
		connected.client.SocketSend(packets.NewDenyResponse("User already exists"))
		return
	}

	genericFailMessage := packets.NewDenyResponse("Failed to register user (internal server error) - please try again later")

	ex, _ := os.Executable()
	base := filepath.Dir(ex)
	err = godotenv.Load(filepath.Join(base, ".env"))

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	// Add new user
	pepper := os.Getenv("PEPPER")
	passwordWithPepper := password + pepper
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(passwordWithPepper), 12)

	connected.logger.Println(pepper)

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	_, err = connected.queries.CreateUser(connected.dbCtx, db.CreateUserParams{
		Username: strings.ToLower(username),
		Password: string(passwordHash),
	})

	if err != nil {
		connected.client.SocketSend(genericFailMessage)
		return
	}

	connected.logger.Printf("User %s registered successfully!", username)
	connected.client.SocketSend(packets.NewOkResponse())
}

func validateUserName(username string) error {
	if len(username) <= 0 {
		return errors.New("empty")
	}

	if len(username) > 20 {
		return errors.New("too long")
	}

	if username != strings.TrimSpace(username) {
		return errors.New("leading or trailing whitespaces")
	}

	return nil
}

func validatePassword(password string, passwordConfirmation string) error {
	if password != passwordConfirmation {
		return errors.New("passwords do not match")
	}

	if len(password) < 8 {
		return errors.New("too short")
	}

	return nil
}