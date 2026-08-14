package middleware

import (
	"errors"
)

// Ref: https://github.com/gofiber/fiber/tree/master/middleware/timeout
var ErrFooTimeOut = errors.New("foo context canceled")
