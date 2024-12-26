# Create an api that is converting the docx, xlsx, odt files into HTML

> Injest information from this file, implement low-level tasks, and generate the code that will satisfy the Objective.

## Objective

Create an API in GoLang v1.23 that exposes endpoints to convert document files into HTML with help of Libreoffice.

- GET / - same as /panel
- GET /panel - the endpoint serves simple HTML form where we can upload document files, and in response get a converted outcome
- GET /converts - the enpoint gets all saved convert jobs. Responds in JSON
- GET /converts/:convertId - the endpoint returns details of the specific convert job. Responds in JSON
- POST /converts/ - endpoint to to create convert jobs. Responds with job id. Response Location header is set
- GET /convert-outcomes/:convertId - the endpoint to download a convert job outcome, the html file.

## Context

The API will run in the environment where LibreOffice is installed and available to call.

Files:
 - main.go - here we put entire server with conversion code
 - services/db.go - converter database related code
 - templates/panel.html - UI for manual conversion
 - Dockerfile - build and production image where the API will run
 - entrypoint.sh - entrypoint bash script that verifys the environment before API starts

## Low-level tasks

1. **Handle File Upload and Conversion**  
   - Implement file uploading on `POST /converts/`.  
   - Write the code (in `main.go` or separate package) to store incoming files in a temp directory and call LibreOffice for conversion.  
   - Update the job record in the database to reflect conversion status (`pending`, `in_progress`, `complete`, or `failed`).  

2. **Add Worker or Goroutine for Asynchronous Processing**  
   - Refactor conversion to run in a separate worker/goroutine so the main HTTP handler returns quickly.  
   - Ensure that job status updates are still logged to the database so WebSocket can broadcast changes.  

3. **Integrate WebSocket for Real-Time Progress**  
   - Maintain a list of connected WebSocket clients.  
   - On each job status change (in the worker), send an update to all connected clients.  
   - Keep existing logic or refactor it so that the broadcast function can be invoked from the conversion worker.  

4. **Introduce Background Cleanup Job**  
   - Create a function that periodically checks for old jobs (define “old” by a cutoff time or status).  
   - Ensure the cleanup job removes both the database record and associated files/directories.  
   - Decide how to configure the cutoff (e.g., via environment variables or a constant).  
   - Make sure the background cleanup runs at a set interval and gracefully stops when the application shuts down.  

5. **Database Modifications**  
   - In `services/db.go`, add methods to retrieve old jobs (e.g., older than N days/weeks) and delete them.  
   - Update job records upon each step of the conversion.  

6. **Graceful Shutdown**  
   - Use a context or similar approach to stop the worker pool and cleanup job gracefully (e.g., on SIGTERM or interrupt).  
   - Close the database connection and any other resources in the shutdown sequence.  

7. **Dockerfile and Entrypoint Adjustments**  
   - Ensure the final Docker image still has LibreOffice installed and accessible.  
   - If any environment variables are needed (e.g., for cleanup intervals), set or expose them in `Dockerfile` or `entrypoint.sh`.  

> **Note:**  
> - Place all modifications for scheduling/cleanup in `main.go` or a new worker file if preferred.  
> - Use database calls and environment-variable checks inside the cleanup logic to determine which jobs to remove.  
> - Do not block the server in the background tasks—run them in separate goroutines or worker threads.  





