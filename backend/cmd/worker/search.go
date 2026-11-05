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
	adminSvc := searchapp.NewAdminService(osClient, searchinfra.NewPostgresSearchStore(a.pool.Pool), freshness, a.tel.Logger)

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
		indexFlag := fs.String("index", "", "Nombre del alias/índice a reindexar (pets|clinical|medications|organizations)")
		aliasFlag := fs.String("alias", "", "Nombre del alias a reindexar (pets|clinical|medications|organizations)")
		reasonFlag := fs.String("reason", "", "Motivo justificado del reindex por auditoría")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		target := *aliasFlag
		if target == "" {
			target = *indexFlag
		}
		if target == "" || *reasonFlag == "" {
			return fmt.Errorf("--index (o --alias) y --reason son obligatorios")
		}

		fmt.Printf("Iniciando reindexación de %q (motivo: %q)...\n", target, *reasonFlag)
		jobID, err := adminSvc.Reindex(ctx, target, *reasonFlag, nil)
		if err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
		fmt.Printf("Reindex iniciado. Job ID: %s\n", jobID)
		return nil

	case "mapping-check":
		reports, err := adminSvc.MappingCheck(ctx)
		if err != nil {
			return fmt.Errorf("mapping-check: %w", err)
		}
		hasDrift := false
		for _, r := range reports {
			if r.Drift {
				hasDrift = true
				fmt.Printf("[DRIFT DETECTADO] Alias: %-15s Índice: %-25s Diferencias: %s\n", r.Alias, r.PhysicalIndex, r.Differences)
			} else {
				fmt.Printf("[OK] Alias: %-15s Índice: %-25s Sin drift de mapping\n", r.Alias, r.PhysicalIndex)
			}
		}
		if hasDrift {
			return fmt.Errorf("se detectó drift en los mappings de OpenSearch; planifique un reindex sin downtime (ARQ-003) antes de desplegar")
		}
		fmt.Println("Todos los mappings coinciden con los esquemas versionados del repositorio.")
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
		return fmt.Errorf("subcomando de búsqueda desconocido: %s (init|reindex|mapping-check|status)", args[0])
	}
}
