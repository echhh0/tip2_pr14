package graph

import (
	"tip2/services/graphql/graph/model"
	"tip2/services/graphql/internal/client/tasksclient"
)

func toGraphTask(task tasksclient.Task) *model.Task {
	return &model.Task{
		ID:          task.ID,
		Title:       task.Title,
		Description: emptyToNil(task.Description),
		DueDate:     emptyToNil(task.DueDate),
		Done:        task.Done,
		CreatedAt:   task.CreatedAt,
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
