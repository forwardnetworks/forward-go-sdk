package forward_test

import (
	"context"
	"fmt"
	"log"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func ExampleClient() {
	client, err := forward.NewClient(forward.Config{
		BaseURL:  "https://fwd.example.com",
		APIToken: "access-key:secret",
	})
	if err != nil {
		log.Fatal(err)
	}

	version, _, err := client.Version.Get(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(version.Version)
}
