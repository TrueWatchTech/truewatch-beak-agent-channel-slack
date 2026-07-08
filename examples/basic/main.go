package main

import (
	"fmt"

	beak "github.com/TrueWatchTech/truewatch-beak-agent-channel-slack"
)

func main() {
	connector := beak.NewConnector()
	fmt.Println(connector.Metadata().Label)
}
