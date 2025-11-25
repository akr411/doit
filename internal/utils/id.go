package utils

import (
	"fmt"
	"strconv"

	"github.com/akr411/doit/internal/storage"
)

func ResolveID(store *storage.Storage, idOrIndex string) (string, error) {
	if _, err := strconv.Atoi(idOrIndex); err == nil {
		index, _ := strconv.Atoi(idOrIndex)
		if index < 1 {
			return "", fmt.Errorf("index must be >= 1")
		}

		todos, err := store.GetAllTodos()
		if err != nil {
			return "", err
		}

		if index > len(todos) {
			return "", fmt.Errorf("index %d out of range (1-%d)", index, len(todos))
		}

		return todos[index-1].ID, nil
	}

	todo, err := store.GetTodo(idOrIndex)
	if err != nil {
		return "", fmt.Errorf("todo not found")
	}
	return todo.ID, nil
}
