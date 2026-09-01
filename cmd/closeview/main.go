package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Dylan-Liew/closeview/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "closeview: %v\n", err)
		os.Exit(1)
	}
}
