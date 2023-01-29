package handlers

import (
	"fmt"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-ls/internal/langserver"
	"github.com/hashicorp/terraform-ls/internal/state"
	"github.com/hashicorp/terraform-ls/internal/terraform/exec"
	"github.com/hashicorp/terraform-ls/internal/walker"
	"github.com/stretchr/testify/mock"
)

func TestRename_basic(t *testing.T) {
	tmpDir := TempDir(t)

	ss, err := state.NewStateStore()
	if err != nil {
		t.Fatal(err)
	}
	wc := walker.NewWalkerCollector()

	ls := langserver.NewLangServerMock(t, NewMockSession(&MockSessionInput{
		TerraformCalls: &exec.TerraformMockCalls{
			PerWorkDir: map[string][]*mock.Call{
				tmpDir.Path(): {
					{
						Method:        "Version",
						Repeatability: 1,
						Arguments: []interface{}{
							mock.AnythingOfType(""),
						},
						ReturnArguments: []interface{}{
							version.Must(version.NewVersion("0.12.0")),
							nil,
							nil,
						},
					},
					{
						Method:        "GetExecPath",
						Repeatability: 1,
						ReturnArguments: []interface{}{
							"",
						},
					},
				},
			},
		},
		StateStore:      ss,
		WalkerCollector: wc,
	}))
	stop := ls.Start(t)
	defer stop()

    // TODO: does this have to be updated?
	ls.Call(t, &langserver.CallRequest{
		Method: "initialize",
		ReqParams: fmt.Sprintf(`{
	    "capabilities": {
	    	"definition": {
	    		"linkSupport": true
	    	}
	    },
	    "rootUri": %q,
	    "processId": 12345
	}`, tmpDir.URI)})
	waitForWalkerPath(t, ss, wc, tmpDir)
	ls.Notify(t, &langserver.CallRequest{
		Method:    "initialized",
		ReqParams: "{}",
	})
	ls.Call(t, &langserver.CallRequest{
		Method: "textDocument/didOpen",
		ReqParams: fmt.Sprintf(`{
		"textDocument": {
			"version": 0,
			"languageId": "terraform",
			"text": `+fmt.Sprintf("%q",
			`variable "test" {
}

output "foo" {
  value = "${var.test}-${var.test}"
}`)+`,
			"uri": "%s/main.tf"
		}
	}`, tmpDir.URI)})
	waitForAllJobs(t, ss)

    // TODO: Update result here
	ls.CallAndExpectResponse(t, &langserver.CallRequest{
		Method: "textDocument/rename",
		ReqParams: fmt.Sprintf(`{
			"textDocument": {
				"uri": "%s/main.tf"
			},
			"position": {
				"line": 0,
				"character": 2
			},
            "newName": "foobar"
		}`, tmpDir.URI)}, fmt.Sprintf(`{
			"jsonrpc": "2.0",
			"id": 3,
			"result": {
                "changes": {
                    "%s/main.tf": [
                        {
                            "newText": foobar",
                            "range": {
                                "start": {
                                    "line": 4,
                                    "character": 13
                                },
                                "end": {
                                    "line": 4,
                                    "character": 21
                                }
                            }
                        },
                        {
                            "newText": foobar",
                            "range": {
                                "start": {
                                    "line": 4,
                                    "character": 13
                                },
                                "end": {
                                    "line": 4,
                                    "character": 21
                                }
                            }
                        },
                        {
                            "uri": "%s/main.tf",
                            "range": {
                                "start": {
                                    "line": 4,
                                    "character": 25
                                },
                                "end": {
                                    "line": 4,
                                    "character": 33
                                }
                            }
                        }
                    ]
                }
            }
		}`, tmpDir.URI, tmpDir.URI))
}

