package main

import (
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"go.uber.org/zap"

	"tip2/services/graphql/graph"
	"tip2/services/graphql/graph/generated"
	"tip2/services/graphql/internal/auth"
	"tip2/services/graphql/internal/client/tasksclient"
	sharedlogger "tip2/shared/logger"
	"tip2/shared/middleware"
)

func main() {
	port := getEnv("GRAPHQL_PORT", "8090")
	tasksBaseURL := getEnv("TASKS_REST_BASE_URL", "http://localhost:8082")

	logger, err := sharedlogger.New("graphql")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	tasksClient := tasksclient.New(tasksBaseURL)

	srv := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{
		Resolvers: &graph.Resolver{
			TasksClient: tasksClient,
		},
	}))

	mux := http.NewServeMux()

	mux.Handle("/", playground.Handler("GraphQL Playground", "/query"))
	mux.Handle("/query", srv)

	mux.Handle("/graphql/", playground.Handler("GraphQL Playground", "/graphql/query"))
	mux.Handle("/graphql/query", srv)

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "graphql",
		})
	})

	app := middleware.RequestID(
		middleware.AccessLog(logger)(
			withHeadersInContext(mux),
		),
	)

	addr := ":" + port

	logger.Info(
		"graphql service starting",
		zap.String("address", addr),
		zap.String("tasks_rest_base_url", tasksBaseURL),
	)

	if err := http.ListenAndServe(addr, app); err != nil {
		logger.Fatal("graphql service failed", zap.Error(err))
	}
}

func withHeadersInContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if value := r.Header.Get("Authorization"); value != "" {
			ctx = auth.WithAuthorization(ctx, value)
		}
		if value := r.Header.Get("X-Request-ID"); value != "" {
			ctx = auth.WithRequestID(ctx, value)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
