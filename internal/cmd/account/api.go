package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/jsonout"
)

//go:generate go run api_titles_gen.go

// apiReference is where every method, its request and its answer are documented.
const apiReference = "https://pethost.dev/docs/api/"

// apiCommand is `pethost api [Method] [json]`: any call of the API by its name, raw. The
// methods are the SDK client's own, found by reflection, so a method the SDK gains is a method
// of this command with no line written here.
func apiCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "api [Method] [json]",
		Short: "Call any API method, raw",
		Long: `Call a method of the API by its name, and print its answer as the API's JSON.

The request is the API's JSON too: the argument after the method, or stdin when that
argument is "-". Without one the request is empty. A field the API does not have is
refused, never sent. A stream prints an answer a line until Ctrl-C.

Without a method, every method is listed. Their requests and answers:
` + apiReference,
		Example: `  pethost api
  pethost api GetMachine
  pethost api GetProject '{"project_id": "recipes"}'
  pethost api DeployProject - < request.json
  pethost api TailContainerLogs '{"project_id": "recipes"}'`,
		Args:              cobra.MaximumNArgs(2),
		ValidArgsFunction: completeAPIMethods,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				listAPIMethods(a)
				return nil
			}
			return callAPI(cmd.Context(), a, args[0], args[1:])
		},
	}
}

// apiMethods are the API's calls by name: each method of the SDK's client that takes a context
// and a request.
func apiMethods() map[string]reflect.Method {
	client := reflect.TypeFor[*pethost.Client]()
	methods := map[string]reflect.Method{}
	for i := range client.NumMethod() {
		if method := client.Method(i); isAPICall(method.Type) {
			methods[method.Name] = method
		}
	}
	return methods
}

// isAPICall says whether a method of the client has a call's shape: the client, a context and
// a request.
func isAPICall(method reflect.Type) bool {
	return method.NumIn() == 3 && method.In(1) == reflect.TypeFor[context.Context]() && method.In(2).Kind() == reflect.Pointer
}

// apiMethodNames are the methods in the API's own order, which the SDK's documentation has
// (apiTitles); one that the documentation read at build time lacks comes last.
func apiMethodNames() []string {
	methods := apiMethods()
	var names []string
	for _, titled := range apiTitles {
		if _, ok := methods[titled.name]; ok {
			names = append(names, titled.name)
			delete(methods, titled.name)
		}
	}
	return append(names, slices.Sorted(maps.Keys(methods))...)
}

func apiTitleOf(name string) string {
	for _, titled := range apiTitles {
		if titled.name == name {
			return titled.title
		}
	}
	return ""
}

func listAPIMethods(a *app.App) {
	table := a.UI.Out.Table()
	for _, name := range apiMethodNames() {
		table.Row(name, apiTitleOf(name))
	}
	table.Flush()
	a.UI.Info("Their requests and answers: %s", apiReference)
}

func completeAPIMethods(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []cobra.Completion
	for _, name := range apiMethodNames() {
		completions = append(completions, cobra.CompletionWithDesc(name, apiTitleOf(name)))
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func callAPI(ctx context.Context, a *app.App, name string, rest []string) error {
	method, err := findAPIMethod(name)
	if err != nil {
		return err
	}
	request, err := readAPIRequest(a, method, rest)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	results := method.Func.Call([]reflect.Value{reflect.ValueOf(client), reflect.ValueOf(ctx), request})
	if len(results) == 1 {
		err := printAPIStream(a, results[0])
		if ctx.Err() != nil {
			return nil // Ctrl-C is how a stream is left, as `pethost logs -f` is: it ended well.
		}
		return err
	}
	if err, _ := results[1].Interface().(error); err != nil {
		return err
	}
	return printAPIAnswer(a, results[0].Interface())
}

// findAPIMethod finds a method by its name, in any case: the API's reference writes
// QueryHttpTraffic where Go writes QueryHTTPTraffic. A name that is no method's is answered
// with the nearest ones.
func findAPIMethod(name string) (reflect.Method, error) {
	var near []string
	for _, candidate := range apiMethodNames() {
		if strings.EqualFold(candidate, name) {
			return apiMethods()[candidate], nil
		}
		if isNear(strings.ToLower(name), strings.ToLower(candidate)) {
			near = append(near, "pethost api "+candidate)
		}
	}

	refusal := fail.Choice(fmt.Sprintf("The API has no method %q.", name), near...)
	if len(near) == 0 {
		return reflect.Method{}, refusal.WithLead("Every method:", "pethost api")
	}
	refusal.Lead = "Did you mean:"
	return reflect.Method{}, refusal
}

// isNear says whether a mistyped name may have meant a method: it is part of the method's name
// ("logs"), or a few letters off it.
func isNear(typed, method string) bool {
	return len(typed) >= 3 && strings.Contains(method, typed) || editDistance(typed, method) <= 3
}

// editDistance is how many letters must be added, dropped or changed to make one word of the
// other.
func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			above := row[j]
			if a[i-1] != b[j-1] {
				diagonal = min(diagonal, row[j], row[j-1]) + 1
			}
			row[j], diagonal = diagonal, above
		}
	}
	return row[len(b)]
}

// readAPIRequest reads the method's request from the API's JSON, which the SDK's types read
// themselves, refusing a field the API does not have. No JSON = the empty request.
func readAPIRequest(a *app.App, method reflect.Method, rest []string) (reflect.Value, error) {
	request := reflect.New(method.Type.In(2).Elem())
	if len(rest) == 0 {
		return request, nil
	}

	text := rest[0]
	if text == "-" {
		piped, err := io.ReadAll(a.UI.In)
		if err != nil {
			return request, err
		}
		text = string(piped)
	}
	if strings.TrimSpace(text) == "" {
		return request, nil
	}

	if err := json.Unmarshal([]byte(text), request.Interface()); err != nil {
		refusal := fail.Choice(fmt.Sprintf("That is not a request of %s: %s.", method.Name, err))
		return request, refusal.WithLead("Its fields:", apiReference)
	}
	return request, nil
}

// printAPIAnswer writes an answer as the API's JSON: indented for a person at a terminal, on
// one line for a program.
func printAPIAnswer(a *app.App, answer any) error {
	if !a.UI.Out.Terminal {
		return a.PrintJSON(answer)
	}
	return jsonout.WriteIndented(a.UI.Out, answer)
}

// printAPIStream ranges over a stream, which the SDK returns as a function to range over, and
// writes each answer on a line of its own. The stream's error, or the first failure to write,
// ends it.
func printAPIStream(a *app.App, stream reflect.Value) error {
	var failure error
	each := reflect.MakeFunc(stream.Type().In(0), func(args []reflect.Value) []reflect.Value {
		if err, _ := args[1].Interface().(error); err != nil {
			failure = err
		} else {
			failure = a.PrintJSON(args[0].Interface())
		}
		return []reflect.Value{reflect.ValueOf(failure == nil)}
	})
	stream.Call([]reflect.Value{each})
	return failure
}

// An apiTitle is a method and the first line of its documentation in the SDK.
type apiTitle struct {
	name, title string
}
