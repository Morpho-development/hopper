package httpapi

import (
	"net/http"
	"testing"
)

func TestMessageCreateListDelete(t *testing.T) {
	router := NewRouter(testConfig(t), testDB(t))
	token := registerAndLogin(t, router)

	create := doJSON(t, router, http.MethodPost, "/api/messages/", map[string]string{
		"body": "<script>alert('text only')</script>",
	}, token)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var message struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	decodeResponse(t, create, &message)
	if message.ID == "" || message.Body != "<script>alert('text only')</script>" {
		t.Fatalf("unexpected message: %+v", message)
	}

	list := doJSON(t, router, http.MethodGet, "/api/messages/", nil, token)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var listed struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	decodeResponse(t, list, &listed)
	if len(listed.Messages) != 1 || listed.Messages[0].ID != message.ID {
		t.Fatalf("unexpected message list: %+v", listed.Messages)
	}

	deleted := doJSON(t, router, http.MethodDelete, "/api/messages/"+message.ID, nil, token)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.Code)
	}
}
