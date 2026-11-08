package domain

import (
	"errors"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	ErrBudgetExceeded     = kernel.NewDomainError("AI_BUDGET_EXCEEDED", "Se ha superado el presupuesto mensual de IA. Por favor, realiza la captura manual de la receta.")
	ErrQualityTooLow      = kernel.NewDomainError("REUPLOAD_REQUIRED", "La calidad de la imagen es insuficiente para el análisis automático. Por favor, sube una nueva foto.")
	ErrExtractionRefused  = kernel.NewDomainError("EXTRACTION_REFUSED", "El modelo de IA rechazó analizar el documento.")
	ErrExtractionFailed   = kernel.NewDomainError("EXTRACTION_FAILED", "No fue posible extraer la información de la receta.")
	ErrRetryLimitExceeded = kernel.NewDomainError("EXTRACTION_RETRY_LIMIT", "Se ha alcanzado el límite máximo de reintentos de extracción (3).")
	ErrInvalidDocument    = errors.New("documento no válido o sin páginas legibles")
)
