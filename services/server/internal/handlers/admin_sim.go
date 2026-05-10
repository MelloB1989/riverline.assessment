package handlers

import (
	rivereval "riverline_server/internal/eval"

	"github.com/gofiber/fiber/v2"
)

func AdminLearningStart(c *fiber.Ctx) error {
	var req rivereval.SupervisorConfig
	_ = c.BodyParser(&req)
	if err := rivereval.StartSupervisor(req); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return c.JSON(rivereval.CurrentSupervisorStatus())
}

func AdminLearningStop(c *fiber.Ctx) error {
	if err := rivereval.StopSupervisor(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(rivereval.CurrentSupervisorStatus())
}

func AdminLearningStatus(c *fiber.Ctx) error {
	return c.JSON(rivereval.CurrentSupervisorStatus())
}

func AdminSingleSimulation(c *fiber.Ctx) error {
	var req rivereval.SingleSimulationRequest
	_ = c.BodyParser(&req)
	resp, err := rivereval.RunSingleSimulation(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(resp)
}
