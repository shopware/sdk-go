# Using DynamoDB Local for Development

This guide explains how to use Amazon DynamoDB Local for local development instead of SQLite or AWS DynamoDB.

## What is DynamoDB Local?

DynamoDB Local is a downloadable version of Amazon DynamoDB that enables you to develop and test applications without accessing the actual AWS service. It's perfect for local development and testing.

## Quick Start

### 1. Enable DynamoDB Local in `docker-compose.yml`

Uncomment the `dynamodb-local` service and volume:

```yaml
services:
  # ... existing app service ...

  dynamodb-local:
    image: amazon/dynamodb-local:latest
    container_name: dynamodb-local
    ports:
      - "8000:8000"
    command: "-jar DynamoDBLocal.jar -sharedDb"
    volumes:
      - dynamodb-data:/home/dynamodblocal/data
    restart: unless-stopped

volumes:
  shop-data:
    driver: local
  dynamodb-data:  # Uncomment this
    driver: local
```

### 2. Update Environment Variables

Create a `.env` file or update your environment:

```bash
# Store Configuration
STORE_TYPE=dynamodb

# DynamoDB Configuration
DYNAMODB_TABLE=shopware-shops
DYNAMODB_REGION=us-east-1
DYNAMODB_ENDPOINT=http://dynamodb-local:8000

# Dummy AWS credentials (DynamoDB Local doesn't validate these)
AWS_ACCESS_KEY_ID=dummy
AWS_SECRET_ACCESS_KEY=dummy
```

### 3. Start Services

```bash
docker compose up -d
```

### 4. Create the DynamoDB Table

The SDK automatically creates the table if it doesn't exist, but you can also create it manually:

```bash
# Install AWS CLI if you haven't already
# brew install awscli

# Create table
aws dynamodb create-table \
    --table-name shopware-shops \
    --attribute-definitions \
        AttributeName=id,AttributeType=S \
    --key-schema \
        AttributeName=id,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    --endpoint-url http://localhost:8000 \
    --region us-east-1
```

## Accessing DynamoDB Local

### From Your Host Machine

```bash
# List tables
aws dynamodb list-tables \
    --endpoint-url http://localhost:8000 \
    --region us-east-1

# Scan all shops
aws dynamodb scan \
    --table-name shopware-shops \
    --endpoint-url http://localhost:8000 \
    --region us-east-1

# Get specific shop
aws dynamodb get-item \
    --table-name shopware-shops \
    --key '{"id": {"S": "YOUR_SHOP_ID"}}' \
    --endpoint-url http://localhost:8000 \
    --region us-east-1
```

### From Within Docker Containers

Use the service name `dynamodb-local`:

```bash
DYNAMODB_ENDPOINT=http://dynamodb-local:8000
```

### Using NoSQL Workbench

Download [NoSQL Workbench for DynamoDB](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/workbench.html) for a GUI:

1. Download and install NoSQL Workbench
2. Add connection:
   - **Connection name**: Local DynamoDB
   - **Port**: 8000
   - **Host**: localhost
3. Browse tables and data visually

## Data Persistence

### Persistent Mode (Recommended for Development)

Data is saved to a Docker volume and persists across container restarts:

```yaml
volumes:
  - dynamodb-data:/home/dynamodblocal/data
```

### In-Memory Mode (Faster, No Persistence)

Use this for testing where you don't need data to persist:

```yaml
command: "-jar DynamoDBLocal.jar -sharedDb -inMemory"
```

## Switching Between Stores

You can easily switch between stores by changing the `STORE_TYPE` environment variable:

```bash
# SQLite (default, file-based persistence)
STORE_TYPE=sqlite
DATABASE_PATH=./data/shops.db

# DynamoDB Local (for testing DynamoDB features)
STORE_TYPE=dynamodb
DYNAMODB_ENDPOINT=http://dynamodb-local:8000

# In-Memory (fastest, no persistence)
STORE_TYPE=memory

# AWS DynamoDB (production)
STORE_TYPE=dynamodb
# Remove DYNAMODB_ENDPOINT to use real AWS
AWS_ACCESS_KEY_ID=your-real-key
AWS_SECRET_ACCESS_KEY=your-real-secret
```

## Troubleshooting

### "Cannot connect to DynamoDB"

1. Check if DynamoDB Local is running:
   ```bash
   docker ps | grep dynamodb-local
   ```

2. Verify the endpoint URL:
   ```bash
   curl http://localhost:8000
   # Should return: {"__type":"com.amazon.coral.service#UnknownOperationException",...}
   ```

3. Check Docker network connectivity:
   ```bash
   docker exec shopware-gin-service ping dynamodb-local
   ```

### "Table does not exist"

The SDK should create the table automatically. If not:

```bash
aws dynamodb create-table \
    --table-name shopware-shops \
    --attribute-definitions AttributeName=id,AttributeType=S \
    --key-schema AttributeName=id,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    --endpoint-url http://localhost:8000 \
    --region us-east-1
```

### Resetting DynamoDB Local

To start fresh:

```bash
# Stop and remove containers
docker compose down

# Remove the volume
docker volume rm gin-service_dynamodb-data

# Start again
docker compose up -d
```

## Production Deployment with AWS DynamoDB

When deploying to production, simply remove the `DYNAMODB_ENDPOINT` variable to use real AWS DynamoDB:

```bash
# Production environment variables
STORE_TYPE=dynamodb
DYNAMODB_TABLE=shopware-shops-prod
DYNAMODB_REGION=us-east-1
# DYNAMODB_ENDPOINT is not set - will use real AWS

# Use IAM roles or provide real credentials
AWS_ACCESS_KEY_ID=your-real-access-key
AWS_SECRET_ACCESS_KEY=your-real-secret-key
```

## Performance Comparison

| Store Type | Speed | Persistence | Use Case |
|-----------|-------|-------------|----------|
| **Memory** | ⚡⚡⚡ Fastest | ❌ None | Quick tests, CI/CD |
| **SQLite** | ⚡⚡ Fast | ✅ File-based | Local development (default) |
| **DynamoDB Local** | ⚡ Good | ✅ Docker volume | Testing DynamoDB features |
| **AWS DynamoDB** | ⚡ Good | ✅ Cloud | Production |

## Additional Resources

- [DynamoDB Local Documentation](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.html)
- [AWS CLI DynamoDB Commands](https://docs.aws.amazon.com/cli/latest/reference/dynamodb/)
- [NoSQL Workbench](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/workbench.html)

