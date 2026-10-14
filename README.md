# BloodBank Management System

A friendly bloodbank management demo built with **React + TypeScript**, a **Go + Gin** API, and **PostgreSQL**. Docker Desktop runs PostgreSQL so you do not need to install or configure PostgreSQL separately.

> **Demo only:** use fictional donor and hospital information. Blood group filtering is not a clinical compatibility, eligibility, or transfusion recommendation. This is not production-ready medical software.

## What you will set up

1. Docker Desktop to run the PostgreSQL database
2. Go to run the backend API
3. Node.js and npm to install and run the React frontend
4. Two PowerShell terminals to keep the backend and frontend running

React is included as a project dependency. You do **not** download React separately; `npm install` installs React and the other frontend packages from `frontend/package.json`.

## 1. Install the required tools

Download and install:

- **Docker Desktop for Windows:** [docker.com/products/docker-desktop](https://www.docker.com/products/docker-desktop/)
- **Go 1.25 or newer:** [go.dev/dl](https://go.dev/dl/)
- **Node.js LTS:** [nodejs.org/en/download](https://nodejs.org/en/download/) (npm is included with Node.js)
- **Visual Studio Code (recommended):** [code.visualstudio.com/download](https://code.visualstudio.com/download)

During Docker Desktop setup, enable its recommended **WSL 2** backend if prompted. Start Docker Desktop after installation and wait until it reports that Docker is running. If an installer asks you to restart Windows, restart before continuing.

Open a new PowerShell window and confirm the tools are available:

```powershell
docker --version
docker compose version
docker info
go version
node --version
npm --version
```

`docker info` must show both **Client** and **Server** information. If it reports that it cannot connect to the Docker API, Docker Desktop is not ready yet.

## 2. Get the project and open its root folder

Download and extract the project ZIP, or clone the project if you have its Git URL. If using a ZIP, extract it once.

In VS Code, open the folder that contains all of these:

```text
backend\
frontend\
docker-compose.yml
README.md
```

Open a VS Code terminal with **Terminal → New Terminal**. Check that you are in this project root:

```powershell
Get-Location
Get-ChildItem
```

If you do not see `backend`, `frontend`, and `docker-compose.yml`, change to the correct folder before continuing. The commands below assume PowerShell is at the project root.

## 3. Start PostgreSQL with Docker

Make sure Docker Desktop is running. In the project-root terminal, run:

```powershell
docker compose up -d
docker compose ps
```

The `postgres` service should show as running (and become healthy). This starts PostgreSQL on `localhost:5432` with the local demo database and credentials already set in [docker-compose.yml](./docker-compose.yml).

If port 5432 is already in use by another PostgreSQL service, do not start a second database on that port; use the existing database only if you know its connection details. **Do not run `docker compose down -v`**: removing the volume deletes the database data.

## 4. Configure and start the Go backend

Open a **second terminal** in VS Code. Keep it separate from the project-root terminal used for Docker. In the new terminal, run:

```powershell
cd .\backend
$env:DATABASE_URL = "postgres://bloodconnect:bloodconnect_dev@localhost:5432/bloodconnect?sslmode=disable"
$env:JWT_SECRET = ([guid]::NewGuid().ToString("N") + [guid]::NewGuid().ToString("N"))
$env:BOOTSTRAP_ADMIN_EMAIL = "admin@bbms.com"
$securePassword = Read-Host "Enter the admin password" -AsSecureString
$env:BOOTSTRAP_ADMIN_PASSWORD = [System.Net.NetworkCredential]::new("", $securePassword).Password
go mod download
go run ./cmd/api
```
admin password : bbms2026bc

When prompted, enter the administrator password provided by your project owner. The password is hidden as you type and is not saved in the README. Choose a unique password for any non-demo use. The backend applies the configured admin password when it starts. Keep this terminal open while using the application.

If Go reports `go.mod file not found`, check your current folder with `Get-Location`; you must be in the `backend` folder before running `go` commands.

## 5. Install and start the React frontend

Open a **third terminal** in VS Code. Make sure this terminal starts at the project root, then run:

```powershell
cd .\frontend
npm install
npm run dev
```

Keep the terminal open. Vite prints a local URL, usually **http://127.0.0.1:5173**. Open that URL in your browser.

## 6. Sign in and check that it works

- Choose **Admin login** and use the configured admin email and password.
- Choose **Donor login** to create a donor account or sign in to one.
- Donors can view and update only their own donor profile. Admins can register, edit, and delete donor and hospital records, and edit or delete inventory entries in the management workspace.
- Deleting a donor also deletes their linked donor login account. Hospitals with inventory or blood requests cannot be deleted until those records are resolved.
- Admins can delete fulfilled requests from the request tracker. Deleting a fulfilled request removes its tracker record but does not restore inventory already deducted.

To check the API from another PowerShell terminal:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/v1/health
Invoke-RestMethod http://127.0.0.1:8080/api/v1/health/db
```

The first check confirms that the Go API is responding; the second confirms that it can reach PostgreSQL.

## 7. Stop and restart the application

- Stop the Go API and the frontend by focusing each terminal and pressing **Ctrl+C**.
- Stop PostgreSQL without deleting its saved data by running this from the project root:

  ```powershell
  docker compose stop
  ```

- To start PostgreSQL another time, run `docker compose up -d` from the project root. Then restart the backend and frontend in their respective folders.

## 8. Run checks (optional)

Backend checks, from the `backend` folder:

```powershell
go test ./...
go build ./cmd/api
```

Frontend production build, from the `frontend` folder:

```powershell
npm run build
```

## Troubleshooting

| Problem | What to do |
| --- | --- |
| Docker cannot connect to its API | Start Docker Desktop, wait for it to finish starting, then retry `docker info`. |
| `no configuration file provided` | Run `docker compose up -d` from the project root, the folder containing `docker-compose.yml`. |
| Port 5432 is already allocated | Check whether PostgreSQL is already running. Do not start a second database or delete Docker volumes. |
| `go.mod file not found` | Change to the `backend` folder before running Go commands. |
| Database unavailable on API startup | Check `docker compose ps` and confirm PostgreSQL is healthy and the `DATABASE_URL` matches the local Compose settings. |
| Port 8080 is already in use | Stop the older BloodBank API with Ctrl+C in its terminal, then start this API again. |
| Port 5173 is already in use | Open the alternate local URL printed by Vite. |
| Admin sign-in fails | Check that the backend was started with the expected `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`; restart the backend after changing them. |

## Security and limitations

- Keep passwords and signing secrets in local environment variables; do not commit them to source control.
- The demo stores the login token in browser `sessionStorage`. Production software should consider secure HttpOnly cookies and CSRF protections.
- Hospital-level record ownership is not enforced; do not use hospital accounts for real multi-tenant deployments.
- Analytics and CSV exports use client-side aggregations of authenticated API data.
- Donor contact details are sensitive. Use synthetic data only and do not publish database exports.
- External fonts are optional; the interface falls back to system fonts offline.
