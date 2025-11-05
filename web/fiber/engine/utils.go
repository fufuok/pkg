package engine

import (
	"strings"

	"github.com/fufuok/utils"
	"github.com/gofiber/fiber/v2"
)

func IsJSON(c *fiber.Ctx) bool {
	return strings.HasPrefix(utils.ToLower(c.Get(fiber.HeaderContentType)), fiber.MIMEApplicationJSON)
}

func IsForm(c *fiber.Ctx) bool {
	return strings.HasPrefix(utils.ToLower(c.Get(fiber.HeaderContentType)), fiber.MIMEApplicationForm)
}
