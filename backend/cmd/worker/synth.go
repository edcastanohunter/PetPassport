package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"math/big"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// SynthConfig describe los volúmenes del generador según el perfil (PP-F3-PRF-002).
type SynthConfig struct {
	Clinics    int
	Vets       int
	Owners     int
	Pets       int
	Encounters int
	Cards      int
}

var (
	profilePilot = SynthConfig{
		Clinics:    5,
		Vets:       20,
		Owners:     100,
		Pets:       150,
		Encounters: 300,
		Cards:      200,
	}

	profileScale = SynthConfig{
		Clinics:    200,
		Vets:       1500,
		Owners:     50000,
		Pets:       80000,
		Encounters: 400000,
		Cards:      300000,
	}
)

var petNames = []string{
	"Luna", "Milo", "Bella", "Rocky", "Coco", "Simba", "Nala", "Zeus",
	"Lola", "Toby", "Max", "Thor", "Mia", "Bruno", "Maya", "Leo",
}

var petBreeds = []string{
	"Golden Retriever", "Criollo", "Labrador", "Bulldog Francés", "Poodle",
	"Pastor Alemán", "Siamés", "Persa", "Beagle", "Boxer", "Schnauzer",
}

func synthCommand(args []string) error {
	fs := flag.NewFlagSet("synth", flag.ContinueOnError)
	profileName := fs.String("profile", "pilot", "perfil de datos sintéticos: pilot | scale")
	seed := fs.Int64("seed", 42, "semilla determinista")
	limitPets := fs.Int("pets", 0, "sobrescribe el número de mascotas a generar")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a, err := open(ctx, "petpassport-synth")
	if err != nil {
		return err
	}
	defer a.close()
	logger := a.tel.Logger

	var cfg SynthConfig
	switch *profileName {
	case "pilot":
		cfg = profilePilot
	case "scale":
		cfg = profileScale
	default:
		return fmt.Errorf("perfil desconocido %q (pilot | scale)", *profileName)
	}

	if *limitPets > 0 {
		cfg.Pets = *limitPets
	}

	logger.InfoContext(ctx, "iniciando generación de datos sintéticos vía command bus (PP-F3-PRF-002)",
		"profile", *profileName, "seed", *seed, "pets_target", cfg.Pets)

	// Inicializar dependencias y command bus
	reg := a.store.Registry()
	commands := bus.New()

	identitySvc := identityapp.NewService(identityinfra.NewPostgresRepository(a.pool), logger)
	orgQueries := orginfra.NewQueries(a.pool.Pool, orginfra.NewUserAdapter(identitySvc))
	guardian := petinfra.NewGuardianAdapter(identitySvc)
	orgs := petinfra.NewOrganizationAdapter(orgQueries)
	petRunner := projection.NewRunner()
	petinfra.RegisterSyncProjectors(petRunner, reg, guardian, orgs)

	petService := petapp.NewService(petapp.Options{
		Repo:     petinfra.NewRepository(a.store, reg, nil, nil),
		UoW:      db.NewUnitOfWork(a.pool),
		Projects: petinfra.NewSyncProjector(petRunner),
		Guardian: guardian,
		Orgs:     orgs,
		Events:   a.store,
		Logger:   logger,
	})
	petService.RegisterCommands(commands)

	start := time.Now()
	var createdPets int

	randInt := func(max int) int {
		if max <= 0 {
			return 0
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
		if err != nil {
			return 0
		}
		return int(n.Int64())
	}

	for i := 0; i < cfg.Pets; i++ {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "generación interrumpida por señal", "created_pets", createdPets)
			return ctx.Err()
		default:
		}

		petID := uuid.New()
		guardianID := uuid.New()
		name := petNames[randInt(len(petNames))]
		breed := petBreeds[randInt(len(petBreeds))]
		species := petdomain.SpeciesDog
		if randInt(100) < 30 {
			species = petdomain.SpeciesCat
		}
		sex := petdomain.SexFemale
		if randInt(100) < 50 {
			sex = petdomain.SexMale
		}

		cmd := petdomain.RegisterPet{
			PetID:      petID,
			GuardianID: guardianID,
			Name:       name,
			Species:    string(species),
			Breed:      &breed,
			Sex:        string(sex),
			Neutered:   "unknown",
		}

		if _, err := commands.Dispatch(ctx, cmd); err != nil {
			logger.WarnContext(ctx, "fallo al despachar registro de mascota", "pet_id", petID, "error", err)
			continue
		}
		createdPets++

		if createdPets%50 == 0 || createdPets == cfg.Pets {
			logger.InfoContext(ctx, "progreso de datos sintéticos",
				"created_pets", createdPets,
				"elapsed", time.Since(start).Round(time.Millisecond).String(),
			)
		}
	}

	logger.InfoContext(ctx, "generación de datos sintéticos completada exitosamente (PP-F3-PRF-002)",
		"total_pets", createdPets,
		"duration", time.Since(start).Round(time.Millisecond).String(),
	)
	return nil
}
