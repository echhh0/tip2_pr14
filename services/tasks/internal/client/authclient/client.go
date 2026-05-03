package authclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tip2/proto"
	"tip2/shared/middleware"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Client struct {
	conn   *grpc.ClientConn
	client proto.AuthServiceClient
	logger *zap.Logger
}

func New(addr string, logger *zap.Logger) (*Client, error) {
	conn, err := grpc.NewClient(
		strings.TrimSpace(addr),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client: %w", err)
	}

	return &Client{
		conn:   conn,
		client: proto.NewAuthServiceClient(conn),
		logger: logger,
	}, nil
}

func (c *Client) Verify(ctx context.Context, token string) (bool, int, error) {
	requestID := middleware.GetRequestID(ctx)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if requestID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", requestID)
	}

	resp, err := c.client.Verify(ctx, &proto.VerifyRequest{Token: token})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.Unauthenticated:
				return false, 401, nil
			case codes.DeadlineExceeded:
				c.logger.Error("auth verify timeout", zap.String("request_id", requestID), zap.String("component", "auth_client"), zap.Error(err))
				return false, 503, fmt.Errorf("auth verify timeout: %w", err)
			case codes.Unavailable:
				c.logger.Error("auth service unavailable", zap.String("request_id", requestID), zap.String("component", "auth_client"), zap.Error(err))
				return false, 503, fmt.Errorf("auth service unavailable: %w", err)
			default:
				c.logger.Error("auth grpc error", zap.String("request_id", requestID), zap.String("component", "auth_client"), zap.Error(err))
				return false, 502, fmt.Errorf("auth grpc error: %w", err)
			}
		}

		c.logger.Error("auth grpc request failed", zap.String("request_id", requestID), zap.String("component", "auth_client"), zap.Error(err))
		return false, 502, fmt.Errorf("auth grpc request failed: %w", err)
	}

	return resp.Valid, 200, nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
