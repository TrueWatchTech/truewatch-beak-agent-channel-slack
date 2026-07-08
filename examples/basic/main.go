package main

import (
	"fmt"

	beak "github.com/TrueWatch/beak-agent-channel-slack"
)

func main() {
	connector := beak.NewConnector()
	fmt.Println(connector.Metadata().Label)
}
