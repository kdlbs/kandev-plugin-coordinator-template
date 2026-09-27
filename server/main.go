// Command kandev-plugin-coordinator-template is the backend half of the reference
// coordinator plugin. Kandev owns the gRPC subprocess transport.
package main

import "github.com/kandev/kandev/pkg/pluginsdk"

func main() {
	pluginsdk.Serve(&coordinatorPlugin{})
}
