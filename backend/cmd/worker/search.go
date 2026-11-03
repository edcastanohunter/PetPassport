package main

import (
	"context"
	"flag"
	"fmt"

	searchapp "github.com/edcastanohunter/petpassport/backend/internal/search/app"
	searchinfra "github.com/edcastanohunter/petpassport/backend/internal/search/infra"
)

// searchCommand gestiona la administración de OpenSearch por CLI (M3.1, PP-F3-API-009).
func searchCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: worker search init | worker search reindex --alias <alias> --reason <reason>")
	}

	ctx := context.Background()
	a, err := open(ctx, "worker-search")
	if err != nil {
		return err
	}
	defer a.pool.Close()

	osClient, err := searchapp.NewClient(searchapp.OpenSearchConfig{
		Addresses: a.cfg.Search.Addresses,
		Username:  a.cfg.Search.Username,
		Password:  a.cfg.Search.Password,
	})
	if err != nil {
		return fmt.Errorf("opensearch client: %w", err)
	}

	freshness := searchinfra.NewDBFreshnessProvider(a.pool.Pool)
	adminSvc := searchapp.NewAdminService(osClient, a.pool.Pool, freshness, a.tel.Logger)

	switch args[0] {
	case "init":
		fmt.Println("Inicializando índices y plantillas de OpenSearch...")
		if err := adminSvc.InitIndices(ctx); err != nil {
			return fmt.Errorf("init indices: %w", err)
		}
		fmt.Println("Índices inicializados con éxito.")
		return nil

	case "reindex":
		fs := flag.NewFlagSet("reindex", flag.ExitOnError)
		aliasFlag := fs.String("alias", "", "Nombre del alias a reindexar (pets|clinical|medications|organizations)")
		reasonFlag := fs.String("reason", "", "Motivo justificado del reindex por auditoría")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		if *aliasFlag == "" || *reasonFlag == "" {
			return fmt.Errorf("--alias y --reason son obligatorios")
		}

		fmt.Printf("Iniciando reindexación de %q (motivo: %q)...\n", *aliasFlag, *reasonFlag)
		jobID, err := adminSvc.Reindex(ctx, *aliasFlag, *reasonFlag, nil)
		if err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
		fmt.Printf("Reindex iniciado. Job ID: %s\n", jobID)
		return nil

	case "status":
		summaries, err := adminSvc.ListIndices(ctx)
		if err != nil {
			return fmt.Errorf("list indices: %w", err)
		}
		for _, s := range summaries {
			fmt.Printf("Alias: %-15s Físico: %-25s Docs: %-8d Tamaño: %-10d Drift: %v Lag: %ds\n",
				s.Alias, s.Physical, s.DocsCount, s.SizeBytes, s.Drift, s.LagSeconds)
		}
		return nil

	default:
		return fmt.Errorf("subcomando de búsqueda desconocido: %s (init|reindex|status)", args[0])
	}
}
