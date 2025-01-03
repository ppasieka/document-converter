# Create an api that is converting the docx, xlsx, odt files into HTML

> Injest information from this file, implement low-level tasks, and generate the code that will satisfy the Objective.

## Objective

Create an API in GoLang v1.23 that exposes endpoints to convert document files into HTML with help of Libreoffice.

- GET / - same as /panel
- GET /panel - the endpoint serves simple HTML form where we can upload document files, and in response get a converted outcome
- GET /converts - the enpoint gets all saved convert jobs. Responds in JSON
- GET /converts/:convertId - the endpoint returns details of the specific convert job. Responds in JSON
- DELETE /converts/:convertId - the endpoint to delete the job.
- POST /converts/ - endpoint to to create convert jobs. Responds with job id. Response Location header is set
- GET /convert-outcomes/:convertId - the endpoint to download a convert job outcome, the html file.

## Context

The API will run in the environment where LibreOffice is installed and available to call.

Files:
 - main.go - here we put entire server with conversion code
 - services/db.go - converter database related code
 - templates/panel.html - UI for manual conversion
 - Dockerfile - build and production image where the API will run

## Low-level tasks
- log all outputs from the libreoffice command execution
- ensure that the file is created in the converted folder after libreoffice finish
- if file does not exists, mark job as failed