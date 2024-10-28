package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	socketio "github.com/googollee/go-socket.io"
	"github.com/joho/godotenv"
	"github.com/rs/cors"
)

var ctx = context.Background()

type Sender struct {
	Dp       string `json:"dp"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type Message struct {
	Content string `json:"content"`
	Id      string `json:"id"`
	Room    string `json:"room"`
	Sender  Sender `json:"sender"`
	Time    string `json:"time"`
	Status  string `json:"status"`
}

type getMessage struct {
	UserId string `json:"userId"`
	Id     string `json:"id"`
}

func addMessageToRoom(rdb *redis.Client, roomID string, message Message) error {
	jsonMessage, err := json.Marshal(message)
	if err != nil {
		return err
	}

	// Use a unique score like timestamp to avoid overwriting
	score := float64(time.Now().UnixNano())
	err = rdb.ZAdd(ctx, "room:"+roomID, &redis.Z{
		Score:  score,
		Member: jsonMessage,
	}).Err()
	if err != nil {
		return err
	}

	return nil
}

func getMessagesFromRoom(rdb *redis.Client, roomID string) ([]Message, error) {
	var messages []Message

	jsonMessages, err := rdb.ZRange(ctx, "room:"+roomID, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	for _, jsonMessage := range jsonMessages {
		var message Message
		err := json.Unmarshal([]byte(jsonMessage), &message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, nil
}

func main() {
	// Load the .env file
	err := godotenv.Load(".env")
	if err != nil {
		// log.Fatal("Error loading .env file")
		fmt.Println("Error loading .env file")
	}

	// Set up Redis client
	uri := os.Getenv("REDISURI")
	socketClient := os.Getenv("SOCKET_CLIENT_URL")
	rdb := redis.NewClient(&redis.Options{
		Addr: uri,
	})
	defer rdb.Close()

	server := socketio.NewServer(nil)

	server.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		fmt.Println("Connected:", s.ID())
		return nil
	})

	server.OnEvent("/", "register", func(s socketio.Conn, id string) {
		fmt.Println("Registered:", id)
		s.Join(id)
		server.BroadcastToRoom("/", id, "notice", "You successfully registered.")
	})

	server.OnEvent("/", "getMesages", func(s socketio.Conn, arg getMessage) {
		messages, _ := getMessagesFromRoom(rdb, arg.Id)
		server.BroadcastToRoom("/", arg.UserId, "prevMessages", messages)
	})

	server.OnEvent("/", "message", func(s socketio.Conn, msg Message) {
		fmt.Println("Received message:", msg)
		err := addMessageToRoom(rdb, msg.Room, msg)
		if err != nil {
			log.Fatal(err)
		}
		server.BroadcastToRoom("/", msg.Room, "reply", msg)
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		fmt.Println("Disconnected:", s.ID(), "Reason:", reason)
		// s.Leave("room1")
	})

	go server.Serve()
	defer server.Close()

	// Set up CORS middleware
	fmt.Println("socketClient: ", socketClient)
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{socketClient, ""},
		AllowCredentials: true,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
	}).Handler

	http.Handle("/socket.io/", corsHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Request origin: %s", r.Header.Get("Origin"))
		server.ServeHTTP(w, r)
	})))

	// http.Handle("/", corsHandler(http.FileServer(http.Dir("./public"))))
	http.Handle("/", http.FileServer(http.Dir("./public")))

	log.Println("Serving at localhost:8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
