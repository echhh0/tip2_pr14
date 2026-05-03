package graph

import "tip2/services/graphql/internal/client/tasksclient"

type Resolver struct {
	TasksClient *tasksclient.Client
}
