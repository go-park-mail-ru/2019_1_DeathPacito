package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"../providers/auth"
	"../providers/user_data"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	Port			string							`json:"port"`
	AuthPort		string							`json:"auth_port"`
	APIPrefix		string							`json:"api_prefix"`
	DBName			string							`json:"db_name"`
	WSRoute			string							`json:"ws_route"`
	MessageRoute	string							`json:"message_route"`
	ServerAddress	string							`json:"server_address"`
	AuthDomain		string							`json:"auth_domain"`
	MongoAddr		string							`json:"mongo_addr"`
	CollName		string							`json:"coll_name"`
	CertFile		string							`json:"cert_file"`
	KeyFile			string							`json:"key_file"`
}

var (
	config = &Config{}
)

type Message struct {
	ID     string `json:"_id,omitempty"`
	Login  string `json:"login,omitempty"`
	Avatar string `json:"avatar,omitempty"`
	UID    string `json:"uid"`
	Text   string `json:"text"`
}

type Client struct {
	isConnected bool

	uid    string
	login  string
	avatar string
	conn   *websocket.Conn
}
type MessageJSON struct {
	Status  string     `json:"status"`
	Payload []*Message `json:"payload"`
}

var authProvider auth.JWTProvider = auth.JWTProvider{
	ServerAddress: config.ServerAddress + config.AuthDomain,
	Secure:        false,
	AuthDomain:    config.AuthDomain,
}

var apiProvider user_data.GrpcProvider = user_data.GrpcProvider{
	ServerAddress: config.APIPrefix + config.AuthPort,
}

func ParseAuth(r *http.Request) (string, error) {
	return authProvider.GetUUID(r)
}

var _client *mongo.Client

func dbConnect() (*mongo.Client, error) {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	if _client == nil {
		client, err := mongo.NewClient(options.Client().ApplyURI(config.MongoAddr))
		if err != nil {
			return nil, err
		}
		_client = client
		err = _client.Connect(ctx)
		if err != nil {
			return nil, err
		}
	}
	err := _client.Ping(ctx, nil)
	return _client, err
}

func (client *Client) ReceiveMessage(messageChan chan *Message) {
	defer func() {
		client.isConnected = false
		client.conn.Close()
	}()
	for {
		message := Message{}
		err := client.conn.ReadJSON(&message)
		if err != nil {
			fmt.Printf(err.Error())
			return
		}

		dbClient, err := dbConnect()
		if err != nil {
			fmt.Println("Failed to connect DB")
			return
		}

		ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
		collection := dbClient.Database(config.DBName).Collection(config.CollName)

		message.UID = client.uid
		if client.uid != "" {
			message.Login = client.login
			message.Avatar = client.avatar
		}
		result, err := collection.InsertOne(ctx, message)
		if err != nil {
			fmt.Println("Failed to create message")
		}
		message.ID = result.InsertedID.(primitive.ObjectID).Hex()
		messageChan <- &message
	}
}

func (client *Client) SendMessage(message *Message) {
	client.conn.WriteJSON(*message)
}

func ChatRoom(clientChan chan *Client, messageChan chan *Message) {
	clients := make(map[string]*Client)
	for {
		select {
		case newClient := <-clientChan:
			clients[newClient.uid] = newClient

			newClient.conn.SetCloseHandler(func(code int, text string) error {
				delete(clients, newClient.uid)
				return nil
			})
			fmt.Println("Client joined")
			userData, err := apiProvider.GetUsers([]string{newClient.uid})

			if err == nil && len(userData) != 0 {
				newClient.login = userData[0].Login
				newClient.avatar = userData[0].Avatar
			}
		case message := <-messageChan:
			for _, client := range clients {
				if client.isConnected {
					go client.SendMessage(message)
				}
			}
		}
	}
}

func HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	setupResponse(&w, r)
	if (*r).Method == "OPTIONS" {
		return
	}
	dbClient, err := dbConnect()
	if err != nil {
		fmt.Println("Failed to connect DB")
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)
	collection := dbClient.Database(config.DBName).Collection(config.CollName)
	options := options.Find()
	options.SetLimit(int64(50)).SetSort(bson.M{"_id": -1})

	cursor, err := collection.Find(ctx, bson.D{}, options)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var messages []*Message
	var uids []string
	for cursor.Next(ctx) {
		m := Message{}
		err = cursor.Decode(&m)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if m.UID != "" {
			uids = append(uids, m.UID)
		}

		messages = append(messages, &m)
	}
	users, err := apiProvider.GetUsers(uids)
	userMap := make(map[string]*user_data.User)
	if err == nil {
		for _, user := range users {
			userMap[user.Uid] = user
		}
		for _, msg := range messages {
			if msg.UID != "" {
				user, ok := userMap[msg.UID]
				if ok {
					msg.Login = user.Login
					msg.Avatar = user.Avatar
				}
			}
		}
	}
	messageJSON := MessageJSON{
		Status:  "success",
		Payload: messages,
	}
	json, _ := json.Marshal(messageJSON)
	w.Write(json)

}
func upgraderHandler(w http.ResponseWriter, r *http.Request, clientChan chan *Client, messageChan chan *Message) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// TODO: Implement
			return true
		},
	}

	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := Client{
		isConnected: true,
		conn:        connection,
	}
	uid, err := ParseAuth(r)
	if err == nil {
		client.uid = uid
	}

	go client.ReceiveMessage(messageChan)
	clientChan <- &client
}

func setupResponse(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Credentials", "true")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func InitDB() {
	dbClient, err := dbConnect()
	if err != nil {
		fmt.Println("Failed to connect DB")
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	collection := dbClient.Database(config.DBName).Collection(config.CollName)
	m := Message{Text: "Server started!", UID: ""}
	collection.InsertOne(ctx, m)
}

func main() {
	configBytes, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Readn't: %v", err)
	}

	err = json.Unmarshal(configBytes, config)
	if err != nil {
		log.Fatalf("Unmarshalln't: %v", err)
	}

	messageChan := make(chan *Message)
	clientChan := make(chan *Client)
	go ChatRoom(clientChan, messageChan)
	InitDB()

	http.HandleFunc(config.MessageRoute, HandleGetMessages)
	http.HandleFunc(config.WSRoute, func(w http.ResponseWriter, r *http.Request) {
		upgraderHandler(w, r, clientChan, messageChan)
	})
	useTLS := os.Getenv("USE_TLS")
	if useTLS == "1" {
		http.ListenAndServeTLS(config.Port, config.CertFile, config.KeyFile, nil)
	} else {
		http.ListenAndServe(config.Port, nil)
	}

}
