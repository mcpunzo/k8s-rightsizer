package main

import (
	"fmt"

	"github.com/mcpunzo/k8s-rightsizer/recommendation/reader"
)

func main() {
	reader, err := reader.NewReader("pippo.xls")
	if err != nil {
		fmt.Println("Error creating reader:", err)
		return
	}
	_, err = reader.Read()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Hello World!")
}
