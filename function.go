package fn

import (
	"fmt"
	"net/http"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

func init() {
	functions.HTTP("Hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from go")
	})
}
