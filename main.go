package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
)

type GalleryImage struct {
	Slug, GalleryId, ImageName, ImageType string
}

type GalleryData struct {
	GeneratedOn time.Time `json:"generatedOn"`
	Images      []string  `json:"images"`
}

// use godot package to load/read the .env file and
// return the value of the key
func goDotEnvVariable(key string) string {

	// load .env file
	err := godotenv.Load(".env")

	if err != nil {
		slog.Error("Error loading .env file", "key", key, "error", err)
		panic(err)
	}

	return os.Getenv(key)
}

func main() {

	slog.SetLogLoggerLevel(slog.LevelInfo)

	// Variables
	galleriesDirectory := filepath.Join("content", "galleries")
	accountId := goDotEnvVariable("ACCOUNT_ID")
	r2AccessKeyId := goDotEnvVariable("R2_ACCESS_KEY_ID")
	r2SecretAccessKey := goDotEnvVariable("R2_SECRET_ACCESS_KEY")
	r2Bucket := goDotEnvVariable("R2_BUCKET")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(r2AccessKeyId, r2SecretAccessKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		slog.Error("config.LoadDefaultConfig", "error", err)
		panic(err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountId))
	})

	listObjectsOutput, err := client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: &r2Bucket,
	})
	if err != nil {
		slog.Error("s3.client.ListObjectsV2", "error", err)
		panic(err)
	}

	// Find all matching objects (images) by gallery ID
	data := make(map[string][]GalleryImage)
	re := regexp.MustCompile(`^images/(\d{8}[a-zA-Z0-9-]+)/(\S+)\.(jpg|png)$`)
	for _, object := range listObjectsOutput.Contents {
		// Get key for R2 object
		key := *object.Key
		slog.Debug("Checking", "key", key)

		matches := re.FindStringSubmatch(key)
		if matches != nil {
			slug := matches[0]
			gallery := matches[1]
			imageName := matches[2]
			imageType := matches[3]
			slog.Debug("Match found", "slug", slug, "gallery", gallery, "imageName", imageName, "imageType", imageType)

			if data[gallery] == nil {
				images := make([]GalleryImage, 0)
				data[gallery] = images
			}
			value := GalleryImage{slug, gallery, imageName, imageType}
			data[gallery] = append(data[gallery], value)
		} else {
			slog.Info("Ignoring", "key", key)
		}
	}

	slog.Debug("Got", "data", data)

	// Get all gallery folders
	entries, err := os.ReadDir(galleriesDirectory)
	if err != nil {
		slog.Error("os.ReadDir", "error", err)
		panic(err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			slog.Error("fs.Info", "error", err)
			panic(err)
		}

		// Make sure entry is a directory
		if !info.IsDir() {
			slog.Debug("Not a directory", "id", info.Name())
			continue
		}

		// Make sure data contains directory name (gallery ID)
		if data[info.Name()] == nil {
			slog.Info("Gallery ID not found in image data, skipping...", "id", info.Name())
			continue
		}

		// Get images for gallery
		galleryPath := filepath.Join(galleriesDirectory, info.Name())
		galleryJsonPath := filepath.Join(galleryPath, "gallery.json")
		galleryImages := data[entry.Name()]
		slog.Info("Image(s) found for gallery", "imageCount", len(galleryImages), "galleryName", entry.Name())

		// Get slugs for images
		slugs := make([]string, 0)
		for _, image := range galleryImages {
			slugs = append(slugs, image.Slug)
		}
		galleryData := GalleryData{time.Now(), slugs}

		// Serialize to string
		jsonString, err := json.MarshalIndent(galleryData, "", "  ")
		if err != nil {
			slog.Error("json.MarshalIndent", "error", err)
			panic(err)
		}

		slog.Debug("Serialized", "json", jsonString)

		// Open the file for writing
		file, err := os.Create(galleryJsonPath)
		if err != nil {
			slog.Error("os.Create", "error", err)
			panic(err)
		}
		defer file.Close()

		// Write the string to the file
		_, err = file.WriteString(string(jsonString))
		if err != nil {
			slog.Error("os.File.WriteString", "error", err)
			panic(err)
		}

		slog.Info("Generated gallery data", "galleryName", info.Name())
	}
}
