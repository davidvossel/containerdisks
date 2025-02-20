package images

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"kubevirt.io/containerdisks/cmd/medius/common"
	"kubevirt.io/containerdisks/pkg/api"
	"kubevirt.io/containerdisks/pkg/repository"
)

func NewPromoteImagesCommand(options *common.Options) *cobra.Command {
	options.PromoteImageOptions = common.PromoteImageOptions{
		TargetRegistry: "quay.io/containerdisks",
	}

	promoteCmd := &cobra.Command{
		Use:   "promote",
		Short: "Promote verified containerdisks from one registry to another registry",
		Run: func(cmd *cobra.Command, args []string) {
			results, err := readResultsFile(options.ImagesOptions.ResultsFile)
			if err != nil {
				logrus.Fatal(err)
			}

			resultsChan, err := spawnWorkers(cmd.Context(), options, func(a api.Artifact) (*api.ArtifactResult, error) {
				r, ok := results[a.Metadata().Describe()]
				if !ok {
					return nil, nil
				}
				if r.Err != "" {
					return nil, fmt.Errorf("Artifact %s failed in stage %s: %s", a.Metadata().Describe(), r.Stage, r.Err)
				}
				if r.Stage != StageVerify {
					return nil, nil
				}

				errString := ""
				err := promoteArtifact(cmd.Context(), a, r.Tags, options)
				if err != nil {
					errString = err.Error()
				}

				return &api.ArtifactResult{
					Tags:  r.Tags,
					Stage: StagePromote,
					Err:   errString,
				}, err
			})

			for result := range resultsChan {
				results[result.Key] = result.Value
			}

			if !options.DryRun {
				if err := writeResultsFile(options.ImagesOptions.ResultsFile, results); err != nil {
					logrus.Fatal(err)
				}
			}

			if err != nil {
				logrus.Fatal(err)
			}
		},
	}
	promoteCmd.Flags().StringVar(&options.PromoteImageOptions.SourceRegistry, "source-registry", options.PromoteImageOptions.SourceRegistry, "Registry to pull images from")
	promoteCmd.Flags().StringVar(&options.PromoteImageOptions.TargetRegistry, "target-registry", options.PromoteImageOptions.TargetRegistry, "Registry to promote images to")

	err := promoteCmd.MarkFlagRequired("source-registry")
	if err != nil {
		logrus.Fatal(err)
	}

	return promoteCmd
}

func promoteArtifact(ctx context.Context, artifact api.Artifact, tags []string, options *common.Options) error {
	log := common.Logger(artifact)

	if len(tags) == 0 {
		err := errors.New("No containerdisks to promote")
		log.Error(err)
		return err
	}

	repo := repository.RepositoryImpl{}
	srcRef := path.Join(options.PromoteImageOptions.SourceRegistry, tags[0])
	for _, tag := range tags {
		dstRef := path.Join(options.PromoteImageOptions.TargetRegistry, tag)
		if !options.DryRun {
			log.Infof("Copying %s -> %s", srcRef, dstRef)
			if err := repo.CopyImage(ctx, srcRef, dstRef, options.AllowInsecureRegistry); err != nil {
				log.WithError(err).Error("Failed to copy image")
				return err
			}
		} else {
			log.Infof("Dry run enabled, not copying %s -> %s", srcRef, dstRef)
		}

		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
	}

	return nil
}
