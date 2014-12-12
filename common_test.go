package binder

import (
	"mime/multipart"
	"net/http"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

// These types are mostly contrived examples, but they're used
// across many test cases. The idea is to cover all the scenarios
// that this binding package might encounter in actual use.
type (
	// For basic test cases with a required field
	Post struct {
		Title   string `form:"title" json:"title" binding:"Required"`
		Content string `form:"content" json:"content"`
	}

	// To be used as a nested struct (with a required field)
	Person struct {
		Name  string `form:"name" json:"name" binding:"Required"`
		Email string `form:"email" json:"email"`
	}

	// For advanced test cases: multiple values, embedded
	// and nested structs, an ignored field, and single
	// and multiple file uploads
	BlogPost struct {
		Post
		Id          int                     `form:"id" binding:"Required"`
		Ignored     string                  `form:"-" json:"-"`
		Ratings     []int                   `form:"rating" json:"ratings"`
		Author      Person                  `json:"author"`
		Coauthor    *Person                 `json:"coauthor"`
		HeaderImage *multipart.FileHeader   `form:"headerImage"`
		Pictures    []*multipart.FileHeader `form:"picture"`
		unexported  string                  `form:"unexported"`
	}

	EmbedPerson struct {
		*Person
	}

	SadForm struct {
		AlphaDash    string   `form:"AlphaDash" binding:"AlphaDash"`
		AlphaDashDot string   `form:"AlphaDashDot" binding:"AlphaDashDot"`
		MinSize      string   `form:"MinSize" binding:"MinSize(5)"`
		MinSizeSlice []string `form:"MinSizeSlice" binding:"MinSize(5)"`
		MaxSize      string   `form:"MaxSize" binding:"MaxSize(1)"`
		MaxSizeSlice []string `form:"MaxSizeSlice" binding:"MaxSize(1)"`
		Email        string   `form:"Email" binding:"Email"`
		Url          string   `form:"Url" binding:"Url"`
		UrlEmpty     string   `form:"UrlEmpty" binding:"Url"`
	}
)

func (p Post) ValidateBinder(req *http.Request, errors Errors) Errors {
	if len(p.Title) < 10 {
		errors = append(errors, Error{
			FieldNames:     []string{"Title"},
			Classification: "LengthError",
			Message:        "Life is too short",
		})
	}
	return errors
}

const (
	formContentType = "application/x-www-form-urlencoded"
)
