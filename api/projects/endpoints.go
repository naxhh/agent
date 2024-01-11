package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/eduardooliveira/stLib/core/data/database"
	"github.com/eduardooliveira/stLib/core/models"
	"github.com/eduardooliveira/stLib/core/projects"
	"github.com/eduardooliveira/stLib/core/state"
	"github.com/eduardooliveira/stLib/core/utils"
	"github.com/labstack/echo/v4"
	"github.com/morkid/paginate"
	"gorm.io/gorm"
)

func index(c echo.Context) error {
	config := paginate.Config{
		FieldSelectorEnabled: true,
	}
	pg := paginate.New(config)

	q := database.DB.Debug().Model(&models.Project{}).Preload("Tags")

	if c.QueryParams().Has("name") {
		q.Where("name LIKE ?", fmt.Sprintf("%%%s%%", c.QueryParam("name")))
	}
	if c.QueryParams().Has("tags") {
		for i, t := range strings.Split(c.QueryParam("tags"), ",") {
			q.Joins(fmt.Sprintf("LEFT JOIN project_tags as project_tags%d on project_tags%d.project_uuid = projects.uuid", i, i)).
				Where(fmt.Sprintf("project_tags%d.tag_value = ?", i), t)
		}

	}
	page := pg.With(q).Request(c.Request()).Response(&[]models.Project{})
	if page.RawError != nil {
		log.Println(page.RawError)
		return echo.NewHTTPError(http.StatusInternalServerError, page.RawError.Error())
	}
	return c.JSON(http.StatusOK, page)
}

func list(c echo.Context) error {
	rtn, err := database.GetProjectNames()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, rtn)
}

func show(c echo.Context) error {
	uuid := c.Param("uuid")
	rtn, err := database.GetProject(uuid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, rtn)
}

func showAssets(c echo.Context) error {
	uuid := c.Param("uuid")
	rtn, err := database.GetAssetsByProject(uuid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, rtn)
}

func getAsset(c echo.Context) error {
	project, err := database.GetProject(c.Param("uuid"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	asset, err := database.GetProjectAsset(project.UUID, c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if c.QueryParam("download") != "" {
		return c.Attachment(utils.ToLibPath(fmt.Sprintf("%s/%s", project.FullPath(), asset.Name)), asset.Name)

	}

	return c.Inline(utils.ToLibPath(fmt.Sprintf("%s/%s", project.FullPath(), asset.Name)), asset.Name)
}

func save(c echo.Context) error {
	form, err := c.MultipartForm()
	if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusBadRequest)
	}

	projectPayload := form.Value["payload"]
	if len(projectPayload) != 1 {
		log.Println("more payloads than expected")
		return echo.NewHTTPError(http.StatusBadRequest, errors.New("more payloads than expected"))
	}

	pproject := &models.Project{}

	err = json.Unmarshal([]byte(projectPayload[0]), pproject)
	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := c.Bind(pproject); err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if pproject.UUID != c.Param("uuid") {
		return echo.NewHTTPError(http.StatusBadRequest, errors.New("parameter mismatch"))
	}

	project, err := database.GetProject(c.Param("uuid"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if pproject.Name != project.Name {

		err := utils.Move(project.FullPath(), pproject.FullPath())

		if err != nil {
			log.Println(err)
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	err = state.PersistProject(pproject)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	err = database.UpdateProject(pproject)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, pproject)
}

type CreateProject struct {
	Name             string        `json:"name"`
	Description      string        `json:"description"`
	DefaultImageName string        `json:"default_image_name"`
	Tags             []*models.Tag `json:"tags"`
}

func new(c echo.Context) error {

	form, err := c.MultipartForm()
	if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusBadRequest)
	}

	files := form.File["files"]

	if len(files) == 0 {
		log.Println("No files")
		return c.NoContent(http.StatusBadRequest)
	}

	projectPayload := form.Value["payload"]
	if len(projectPayload) != 1 {
		log.Println("more payloads than expected")
		return c.NoContent(http.StatusBadRequest)
	}

	createProject := &CreateProject{}
	err = json.Unmarshal([]byte(projectPayload[0]), createProject)
	if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusBadRequest)
	}

	fileMap := make(map[string]io.ReadCloser)
	for _, file := range files {
		f, err := file.Open()

		if err != nil {
			log.Println(err)
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		defer f.Close()

		fileMap[file.Filename] = f
	}

	command := projects.NewCreateProjectCommand(
		createProject.Name,
		"/",
		createProject.Description,
		createProject.Tags,
		fileMap,
		createProject.DefaultImageName,
	)

	project, err := projects.CreateProject(command)
	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, struct {
		UUID string `json:"uuid"`
	}{project.UUID})
}

func moveHandler(c echo.Context) error {
	pproject := &models.Project{}

	if err := c.Bind(pproject); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusBadRequest)
	}

	if pproject.UUID != c.Param("uuid") {
		return c.NoContent(http.StatusBadRequest)
	}

	project, err := database.GetProject(pproject.UUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	pproject.Path = filepath.Clean(pproject.Path)
	pproject.Name = project.Name
	err = utils.Move(project.FullPath(), pproject.FullPath())

	if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	project.Path = filepath.Clean(pproject.Path)

	err = state.PersistProject(project)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	err = database.UpdateProject(project)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, struct {
		UUID string `json:"uuid"`
		Path string `json:"path"`
	}{project.UUID, project.Path})
}

func setMainImageHandler(c echo.Context) error {
	pproject := &models.Project{}

	if err := c.Bind(pproject); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusBadRequest)
	}

	if pproject.UUID != c.Param("uuid") {
		return c.NoContent(http.StatusBadRequest)
	}

	project, err := database.GetProject(pproject.UUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if pproject.DefaultImageID != project.DefaultImageID {
		project.DefaultImageID = pproject.DefaultImageID
	}

	err = state.PersistProject(project)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	err = database.UpdateProject(project)

	if err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, struct {
		UUID string `json:"uuid"`
		Path string `json:"path"`
	}{project.UUID, project.DefaultImageID})
}