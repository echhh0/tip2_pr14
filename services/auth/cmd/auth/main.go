package main

import (
	"net"
	"net/http"
	"os"

	"tip2/proto"
	grpcapi "tip2/services/auth/internal/grpc"
	httpapi "tip2/services/auth/internal/http"
	"tip2/services/auth/internal/service"
	sharedlogger "tip2/shared/logger"
	"tip2/shared/middleware"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func main() {
	httpPort := getEnv("AUTH_PORT", "8081")
	grpcPort := getEnv("AUTH_GRPC_PORT", "50051")

	logger, err := sharedlogger.New("auth")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	authService := service.New()

	httpHandler := httpapi.New(authService, logger)
	httpMux := http.NewServeMux()
	httpHandler.Register(httpMux)
	httpApp := middleware.RequestID(
		middleware.SecurityHeaders(
			middleware.AccessLog(logger)(httpMux),
		),
	)

	go func() {
		addr := ":" + httpPort
		logger.Info("auth http service starting", zap.String("address", addr))

		if err := http.ListenAndServe(addr, httpApp); err != nil {
			logger.Fatal("auth http service failed", zap.Error(err), zap.String("component", "http_server"))
		}
	}()

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		logger.Fatal("grpc listen failed", zap.Error(err), zap.String("component", "grpc_server"))
	}

	grpcServer := grpc.NewServer()
	grpcHandler := grpcapi.New(authService, logger)
	proto.RegisterAuthServiceServer(grpcServer, grpcHandler)

	logger.Info("auth grpc service starting", zap.String("address", ":"+grpcPort))

	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatal("auth grpc service failed", zap.Error(err), zap.String("component", "grpc_server"))
	}
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
