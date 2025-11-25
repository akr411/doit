package main

import (
	"strconv"

	"github.com/akr411/doit/internal/ui"
)

func runInteractive() error {
	pageSize := getPageSize()
	return ui.RunMainInteractive(store, pageSize)
}

func getPageSize() int {
	pageSizeStr, err := store.GetConfig("pagination_size")
	if err != nil || pageSizeStr == "" {
		return 10
	}
	size, err := strconv.Atoi(pageSizeStr)
	if err != nil {
		return 10
	}
	return size
}
