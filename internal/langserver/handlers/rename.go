package handlers

import (
	"fmt"
	"context"

	"github.com/hashicorp/hcl-lang/lang"
	ilsp "github.com/hashicorp/terraform-ls/internal/lsp"
	lsp "github.com/hashicorp/terraform-ls/internal/protocol"
)

func (svc *service) TextDocumentRename(ctx context.Context, params lsp.RenameParams) (lsp.WorkspaceEdit, error) {
    edits := lsp.WorkspaceEdit{}

    // TODO: check new name is set, else error

    // TODO: Get this working then generalize with references
	dh := ilsp.HandleFromDocumentURI(params.TextDocument.URI)
	doc, err := svc.stateStore.DocumentStore.GetDocument(dh)
	if err != nil {
		return edits, err
	}

    // TODO: This would be the parameter for position, different than
	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return edits, err
	}

	path := lang.Path{
		Path:       doc.Dir.Path(),
		LanguageID: doc.LanguageID,
	}

	origins := svc.decoder.ReferenceOriginsTargetingPos(path, doc.Filename, pos)
    refs_locations := ilsp.RefOriginsToLocations(origins)

    // TODO: Check if any references, maybe display (but not error) when there are no references

    /* TODO: Have to rethink this a little. Need full text of source to make sure
    // that things are working, like running a 'Get definition' on one of the
    // references, then getting base text to make sure it's local or variable,
    // else do nothing
    //
    */
    for _, ref_location := range refs_locations {
        // Setup new text edit
        text_edit := lsp.TextEdit{
           Range: ref_location.Range,
           NewText: params.NewName,
        }

        edit_list, key_exists := edits.Changes[ref_location.URI]
        // If URI in map append, otherwise, make a new map entry
        if key_exists {
            edit_list = append(edit_list, text_edit)
        } else {
            edit_list = []lsp.TextEdit{ text_edit }

        }
        edits.Changes[ref_location.URI] = edit_list
    }

    // TODO, finish this

    return edits, nil

}
