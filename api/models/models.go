package models

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	_ "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	_ "go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	_ "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	_ "go.mongodb.org/mongo-driver/mongo/options"
)

type Model interface {
	Delete() error
	Save() error
}

type User struct {
	Uuid         primitive.ObjectID `bson:"_id,omitempty"`
	Login        string             `bson:"login"`
	PasswordHash string             `bson:"password_hash,omitempty"`
	Email        string             `bson:"email,omitempty"`
	Avatar       string             `bson:"avatar,omitempty"`
	Score        int                `bson:"score"`
}

type Session struct {
	Sid  string
	User *User
}

var Sessions map[string]Session

func dbConnect() (*mongo.Collection, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))

	if err != nil {
		return nil, err
	}

	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Connect(ctx)

	if err != nil {
		return nil, err
	}
	return client.Database("kpacubo").Collection("users"), nil
}

func (session *Session) Save() error {
	Sessions[session.Sid] = *session
	return nil
}

func (user *User) Save() error {
	collection, err := dbConnect()
	if err != nil {
		return errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	// TODO(ukhachev): better update
	_, err = collection.ReplaceOne(ctx, bson.D{{"_id", user.Uuid}}, user)

	return err
}

func getUser(findOptions bson.D) (*User, error) {
	collection, err := dbConnect()
	if err != nil {

		return nil, errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	user := User{}
	collection.FindOne(ctx, findOptions).Decode(&user)
	return &user, nil
}
func GetUser(id primitive.ObjectID) (*User, error) {
	return getUser(bson.D{{"_id", id}})
}
func GetUserByLogin(login string) (*User, error) {
	return getUser(bson.D{{"login", login}})
}

func GetSession(id string) (*Session, error) {
	session, exists := Sessions[id]
	if !exists {
		return nil, errors.New("Wrong sid")
	}
	return &session, nil
}

func GetUsers(count, page int) ([]User, error) {
	if page < 1 {
		return nil, errors.New("invalid page number")
	}
	collection, err := dbConnect()
	if err != nil {
		return nil, errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	options := options.Find()
	options.
		SetLimit(int64(count)).
		SetSkip(int64(page-1) * int64(count)).
		SetSort(bson.M{"score": -1, "login": 1}).
		SetProjection(bson.M{"login": 1, "score": 1})

	cursor, err := collection.Find(ctx, bson.D{}, options)
	if err != nil {
		return nil, errors.New("DB error")
	}
	if !cursor.Next(ctx) {
		return nil, errors.New("not enough users")
	}
	defer cursor.Close(ctx)

	var userSlice []User
	for {
		var user User
		err = cursor.Decode(&user)
		if err != nil {
			return nil, err
		}
		userSlice = append(userSlice, user)
		if !cursor.Next(ctx) {
			break
		}
	}
	return userSlice, nil
}

func (session *Session) Delete() error {
	delete(Sessions, session.Sid)
	return nil
}

func (user *User) Delete() error {
	//TODO : implement later
	//delete(UuidUserIndex, user.Uuid)
	//delete(Users, user.Login)
	return nil
}

func NewSession() *Session {
	id := uuid.New().String()
	session := Session{
		Sid:  id,
		User: nil,
	}
	Sessions[id] = session
	return &session
}

func NewUser(login string, password string, email string) (*User, error) {
	if login == "" {
		return nil, errors.New("login")
	}

	if password == "" {
		return nil, errors.New("password")
	}

	if email == "" {
		return nil, errors.New("email")
	}
	collection, err := dbConnect()
	if err != nil {
		return nil, errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	user := User{
		Login:        login,
		PasswordHash: password,
		Email:        email,
		Score:        20,
	}

	result, err := collection.InsertOne(ctx, user)
	if err != nil {
		return nil, errors.New("user already exists")
	}
	user.Uuid = result.InsertedID.(primitive.ObjectID)
	return &user, nil
}

func Auth(login string, password string) (*User, error) {
	collection, err := dbConnect()
	if err != nil {
		return nil, errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	userBytes := collection.FindOne(ctx, bson.D{{"login", login}, {"password_hash", password}})

	var user User
	err = userBytes.Decode(&user)
	if err != nil {
		return nil, errors.New("password")
	}
	return &user, nil
}
func GetUserCount() (int64, error) {
	collection, err := dbConnect()
	if err != nil {
		return 0, errors.New("Fail to connect db")
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	return collection.CountDocuments(ctx, bson.D{})
}
func InitModels() {
	collection, err := dbConnect()
	if err != nil {
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	collection.DeleteMany(ctx, bson.D{})
	Sessions = make(map[string]Session)
}
