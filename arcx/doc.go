// Package arcx provides higher-level request binding, JSON responses, and
// centralized error handling on top of github.com/ryanfowler/arc.
//
// Arcx is intentionally a wrapper around Arc, not a replacement router. Routes
// still use Arc's matching, method handling, subrouters, host routers, mounts,
// path parameters, and standard net/http middleware.
//
// A small JSON endpoint can bind path and query parameters into a typed input
// struct and return an ordinary Go value:
//
//	r := arcx.New()
//
//	r.Get("/users/{id}", arcx.JSON(func(c *arcx.Context, in GetUser) (User, error) {
//		return users.Get(c.Context(), in.ID, in.Include)
//	}))
//
//	type GetUser struct {
//		ID      int64    `param:"id"`
//		Include []string `query:"include"`
//	}
//
// Handlers that need direct control can use Context helpers and return errors:
//
//	r.Post("/users/{id}", func(c *arcx.Context) error {
//		id, err := c.Param("id").Int64()
//		if err != nil {
//			return err
//		}
//
//		var body UpdateUser
//		if err := c.Decode(&body); err != nil {
//			return err
//		}
//
//		user, err := users.Update(c.Context(), id, body)
//		if err != nil {
//			return err
//		}
//
//		return c.JSON(http.StatusOK, user)
//	})
//
// Typed request structs support these binding tags:
//
//	type CreateUser struct {
//		AccountID string         `param:"accountID"`
//		DryRun    bool           `query:"dry_run,default=false"`
//		Token     string         `header:"Authorization"`
//		SessionID string         `cookie:"sid"`
//		Body      CreateUserBody `body:"json"`
//	}
//
// Tag options are "required" and "default=value". Binding errors and JSON
// decode errors produce 400 responses by default. Validation errors produce
// 422 responses when an input implements Validate() error or
// Validate(context.Context) error. Returned HTTPError values control their own
// status, code, and message. WithErrorHandler replaces the default JSON error
// response behavior.
package arcx
