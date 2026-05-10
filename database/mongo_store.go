package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
}

func NewMongoStore(uri string, dbName string) (*MongoStore, error) {
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		return nil, err
	}

	return &MongoStore{
		client: client,
		db:     client.Database(dbName),
	}, nil
}

func (s *MongoStore) Create(ctx context.Context, collection string, data interface{}) (string, error) {
	coll := s.db.Collection(collection)
	res, err := coll.InsertOne(ctx, data)
	if err != nil {
		return "", err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return fmt.Sprintf("%v", res.InsertedID), nil
}

func (s *MongoStore) Find(ctx context.Context, collection string, filter interface{}) ([]map[string]interface{}, error) {
	coll := s.db.Collection(collection)
	
	f := s.prepareFilter(filter)

	cursor, err := coll.Find(ctx, f)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	// Map _id to id for easier frontend use
	for i, res := range results {
		if oid, ok := res["_id"].(primitive.ObjectID); ok {
			results[i]["id"] = oid.Hex()
		}
	}

	return results, nil
}

func (s *MongoStore) Update(ctx context.Context, collection string, filter interface{}, update interface{}) error {
	coll := s.db.Collection(collection)
	f := s.prepareFilter(filter)

	// Remove _id and id from update data to avoid MongoDB errors
	if m, ok := update.(map[string]interface{}); ok {
		delete(m, "_id")
		delete(m, "id")
		update = m
	}

	_, err := coll.UpdateMany(ctx, f, bson.M{"$set": update})
	return err
}

func (s *MongoStore) Delete(ctx context.Context, collection string, filter interface{}) error {
	coll := s.db.Collection(collection)
	f := s.prepareFilter(filter)
	_, err := coll.DeleteMany(ctx, f)
	return err
}

func (s *MongoStore) prepareFilter(filter interface{}) bson.M {
	f := bson.M{}
	if filter != nil {
		if m, ok := filter.(map[string]interface{}); ok {
			for k, v := range m {
				if k == "_id" || k == "id" {
					if idStr, ok := v.(string); ok {
						if oid, err := primitive.ObjectIDFromHex(idStr); err == nil {
							f["_id"] = oid
							continue
						}
					}
				}
				f[k] = v
			}
		}
	}
	return f
}
