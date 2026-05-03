package grpcapi

import (
	"context"
	"errors"
	"time"

	"tip2/proto"
	"tip2/services/auth/internal/service"
	"tip2/shared/middleware"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Server struct {
	proto.UnimplementedAuthServiceServer
	authService *service.AuthService
	logger      *zap.Logger
}

func New(authService *service.AuthService, logger *zap.Logger) *Server {
	return &Server{authService: authService, logger: logger}
}

func (s *Server) Verify(ctx context.Context, req *proto.VerifyRequest) (*proto.VerifyResponse, error) {
	start := time.Now()
	requestID := requestIDFromMetadata(ctx)
	ctx = middleware.WithRequestID(ctx, requestID)

	subject, err := s.authService.Verify(req.Token)
	if err != nil {
		if errors.Is(err, service.ErrUnauthorized) {
			s.logger.Warn(
				"grpc token verification failed",
				zap.String("request_id", requestID),
				zap.String("component", "grpc_handler"),
				zap.String("method", "Verify"),
				zap.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		s.logger.Error(
			"grpc token verification failed",
			zap.String("request_id", requestID),
			zap.String("component", "grpc_handler"),
			zap.String("method", "Verify"),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
			zap.Error(err),
		)
		return nil, status.Error(codes.Internal, "internal error")
	}

	s.logger.Info(
		"grpc request completed",
		zap.String("request_id", requestID),
		zap.String("component", "grpc_handler"),
		zap.String("method", "Verify"),
		zap.Int("status", 0),
		zap.Int64("duration_ms", time.Since(start).Milliseconds()),
	)

	return &proto.VerifyResponse{
		Valid:   true,
		Subject: subject,
	}, nil
}

func requestIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := md.Get("x-request-id")
	if len(values) == 0 {
		return ""
	}

	return values[0]
}
