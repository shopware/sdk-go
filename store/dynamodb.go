package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBConfig holds configuration for DynamoDB store
type DynamoDBConfig struct {
	// TableName is the name of the DynamoDB table
	TableName string

	// Region is the AWS region for DynamoDB
	Region string

	// Endpoint is an optional custom endpoint (e.g., for local DynamoDB)
	// Leave empty to use the default AWS endpoint
	Endpoint string
}

// DynamoDBAPI defines the interface for DynamoDB operations
// This allows for mocking in tests
type DynamoDBAPI interface {
	GetItem(
		ctx context.Context,
		params *dynamodb.GetItemInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.GetItemOutput, error)
	Query(
		ctx context.Context,
		params *dynamodb.QueryInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.QueryOutput, error)
	PutItem(
		ctx context.Context,
		params *dynamodb.PutItemInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.PutItemOutput, error)
	UpdateItem(
		ctx context.Context,
		params *dynamodb.UpdateItemInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(
		ctx context.Context,
		params *dynamodb.DeleteItemInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.DeleteItemOutput, error)
	DescribeTable(
		ctx context.Context,
		params *dynamodb.DescribeTableInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.DescribeTableOutput, error)
}

type DynamoDBStore struct {
	client    DynamoDBAPI
	tableName string
}

type dynamoDBShop struct {
	ShopID                         string `dynamodbav:"id"`
	ShopURL                        string `dynamodbav:"url"`
	ShopSecret                     string `dynamodbav:"secret"`
	ClientID                       string `dynamodbav:"clientId"`
	ClientSecret                   string `dynamodbav:"clientSecret"`
	Active                         bool   `dynamodbav:"active"`
	Confirmed                      *bool  `dynamodbav:"confirmed,omitempty"`
	PendingShopSecret              string `dynamodbav:"pendingShopSecret,omitempty"`
	PendingShopURL                 string `dynamodbav:"pendingShopUrl,omitempty"`
	PreviousShopSecret             string `dynamodbav:"previousShopSecret,omitempty"`
	SecretsRotatedAt               string `dynamodbav:"secretsRotatedAt,omitempty"`
	HasVerifiedWithDoubleSignature bool   `dynamodbav:"hasVerifiedWithDoubleSignature"`
	CreatedAt                      string `dynamodbav:"created_at"`
	UpdatedAt                      string `dynamodbav:"updated_at"`
}

func NewDynamoDBStore(cfg DynamoDBConfig) (ShopStore, error) {
	awsCfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var client *dynamodb.Client
	if cfg.Endpoint != "" {
		client = dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	} else {
		client = dynamodb.NewFromConfig(awsCfg)
	}

	return NewDynamoDBStoreWithClient(client, cfg), nil
}

// NewDynamoDBStoreWithClient creates a DynamoDBStore with a custom client
func NewDynamoDBStoreWithClient(client DynamoDBAPI, cfg DynamoDBConfig) ShopStore {
	return &DynamoDBStore{
		client:    client,
		tableName: cfg.TableName,
	}
}

func (s *DynamoDBStore) GetShopByID(ctx context.Context, shopID string) (*Shop, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: shopID},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get shop %s: %w", shopID, err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	var dbShop dynamoDBShop
	if unmarshalErr := attributevalue.UnmarshalMap(result.Item, &dbShop); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal shop: %w", unmarshalErr)
	}
	applyDynamoDBCompatibilityAliases(&dbShop, result.Item)

	return s.fromDynamoDBShop(&dbShop)
}

func (s *DynamoDBStore) UpdateShop(ctx context.Context, shop *Shop) error {
	dbShop := s.toDynamoDBShop(shop)

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: shop.ShopID},
		},
		UpdateExpression: aws.String(
			"SET #active = :active, confirmed = :confirmed, #u = :url, secret = :secret, clientId = :clientId, " +
				"clientSecret = :clientSecret, pendingShopSecret = :pendingShopSecret, " +
				"pendingShopUrl = :pendingShopUrl, previousShopSecret = :previousShopSecret, " +
				"secretsRotatedAt = :secretsRotatedAt, " +
				"hasVerifiedWithDoubleSignature = :hasVerifiedWithDoubleSignature, " +
				"created_at = if_not_exists(created_at, :created_at), updated_at = :updated_at",
		),
		ExpressionAttributeNames: map[string]string{
			"#active": "active",
			"#u":      "url",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":active":                         &types.AttributeValueMemberBOOL{Value: dbShop.Active},
			":confirmed":                      &types.AttributeValueMemberBOOL{Value: shop.RegistrationConfirmed},
			":url":                            &types.AttributeValueMemberS{Value: dbShop.ShopURL},
			":secret":                         &types.AttributeValueMemberS{Value: dbShop.ShopSecret},
			":clientId":                       &types.AttributeValueMemberS{Value: dbShop.ClientID},
			":clientSecret":                   &types.AttributeValueMemberS{Value: dbShop.ClientSecret},
			":pendingShopSecret":              &types.AttributeValueMemberS{Value: dbShop.PendingShopSecret},
			":pendingShopUrl":                 &types.AttributeValueMemberS{Value: dbShop.PendingShopURL},
			":previousShopSecret":             &types.AttributeValueMemberS{Value: dbShop.PreviousShopSecret},
			":secretsRotatedAt":               &types.AttributeValueMemberS{Value: dbShop.SecretsRotatedAt},
			":hasVerifiedWithDoubleSignature": &types.AttributeValueMemberBOOL{Value: dbShop.HasVerifiedWithDoubleSignature},
			":created_at":                     &types.AttributeValueMemberS{Value: dbShop.CreatedAt},
			":updated_at":                     &types.AttributeValueMemberS{Value: dbShop.UpdatedAt},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to store shop %s: %w", shop.ShopID, err)
	}

	return nil
}

func (s *DynamoDBStore) SetShopActive(ctx context.Context, shopID string, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: shopID},
		},
		UpdateExpression: aws.String("SET #active = :active, updated_at = :updated_at"),
		ExpressionAttributeNames: map[string]string{
			"#active": "active",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":active":     &types.AttributeValueMemberBOOL{Value: active},
			":updated_at": &types.AttributeValueMemberS{Value: now},
		},
		ConditionExpression: aws.String("attribute_exists(id)"),
	})
	if err != nil {
		return fmt.Errorf("failed to update shop active state %s: %w", shopID, err)
	}

	return nil
}

func (s *DynamoDBStore) DeleteShop(ctx context.Context, shopID string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: shopID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete shop %s: %w", shopID, err)
	}

	return nil
}

func (s *DynamoDBStore) HealthCheck(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(s.tableName),
	})
	if err != nil {
		return fmt.Errorf("dynamodb health check failed: %w", err)
	}

	return nil
}

func (s *DynamoDBStore) toDynamoDBShop(shop *Shop) *dynamoDBShop {
	dbShop := &dynamoDBShop{
		ShopID:                         shop.ShopID,
		ShopURL:                        shop.ShopURL,
		ShopSecret:                     shop.ShopSecret,
		ClientID:                       shop.ClientID,
		ClientSecret:                   shop.ClientSecret,
		Active:                         shop.Active,
		Confirmed:                      boolPointer(shop.RegistrationConfirmed),
		PendingShopSecret:              shop.PendingShopSecret,
		PendingShopURL:                 shop.PendingShopURL,
		PreviousShopSecret:             shop.PreviousShopSecret,
		HasVerifiedWithDoubleSignature: shop.HasVerifiedWithDoubleSignature,
		CreatedAt:                      shop.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:                      shop.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if shop.SecretsRotatedAt != nil {
		dbShop.SecretsRotatedAt = shop.SecretsRotatedAt.UTC().Format(time.RFC3339)
	}
	return dbShop
}

func (s *DynamoDBStore) fromDynamoDBShop(dbShop *dynamoDBShop) (*Shop, error) {
	// Parse timestamps, allowing empty values for compatibility with tables created by other SDKs
	var createdAt, updatedAt time.Time
	var err error

	if dbShop.CreatedAt != "" {
		createdAt, err = time.Parse(time.RFC3339, dbShop.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}
	}

	if dbShop.UpdatedAt != "" {
		updatedAt, err = time.Parse(time.RFC3339, dbShop.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at: %w", err)
		}
	}

	shop := &Shop{
		ShopID:                         dbShop.ShopID,
		ShopURL:                        dbShop.ShopURL,
		ShopSecret:                     dbShop.ShopSecret,
		ClientID:                       dbShop.ClientID,
		ClientSecret:                   dbShop.ClientSecret,
		Active:                         dbShop.Active,
		RegistrationConfirmed:          registrationConfirmed(dbShop),
		PendingShopSecret:              dbShop.PendingShopSecret,
		PendingShopURL:                 dbShop.PendingShopURL,
		PreviousShopSecret:             dbShop.PreviousShopSecret,
		HasVerifiedWithDoubleSignature: dbShop.HasVerifiedWithDoubleSignature,
		CreatedAt:                      createdAt,
		UpdatedAt:                      updatedAt,
	}
	if dbShop.SecretsRotatedAt != "" {
		rotatedAt, parseErr := time.Parse(time.RFC3339, dbShop.SecretsRotatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse secretsRotatedAt: %w", parseErr)
		}
		shop.SecretsRotatedAt = &rotatedAt
	}
	return shop, nil
}

func boolPointer(value bool) *bool {
	return &value
}

func registrationConfirmed(shop *dynamoDBShop) bool {
	if shop.Confirmed != nil {
		return *shop.Confirmed
	}

	if shop.PendingShopSecret != "" && shop.ClientID == "" && shop.ClientSecret == "" {
		return false
	}

	return true
}

func applyDynamoDBCompatibilityAliases(dbShop *dynamoDBShop, item map[string]types.AttributeValue) {
	if _, ok := item["pendingShopSecret"]; !ok {
		dbShop.PendingShopSecret = dynamoDBStringValue(item["pendingSecret"])
	}
	if _, ok := item["pendingShopUrl"]; !ok {
		dbShop.PendingShopURL = dynamoDBStringValue(item["pendingUrl"])
	}
	if _, ok := item["previousShopSecret"]; !ok {
		dbShop.PreviousShopSecret = dynamoDBStringValue(item["previousSecret"])
	}
}

func dynamoDBStringValue(value types.AttributeValue) string {
	stringValue, ok := value.(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return stringValue.Value
}
