# Ticket Checker

A Go-based ticket availability monitor that uses headless Chrome to periodically check a target webpage for ticket availability and sends email notifications when tickets become available.

## Features

- Headless Chrome browser automation for reliable page rendering
- Configurable check interval
- Email notifications via SMTP (supports Gmail and other providers)
- Environment variable configuration with `.env` file support

## Prerequisites

- Go 1.21 or later
- Google Chrome or Chromium browser installed

## Installation

```bash
git clone https://github.com/oossccaa/ticket-checker.git
cd ticket-checker
go mod download
```

## Configuration

Create a `.env` file in the project root with the following variables:

```env
# Target URL to monitor
TARGET_URL=https://example.com/tickets

# Email configuration
RECIPIENT_EMAIL=your-email@example.com
SENDER_EMAIL=sender@gmail.com
SENDER_PASSWORD=your-app-password
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587

# Check interval in seconds (default: 60)
CHECK_INTERVAL_SECONDS=60
```

### Gmail Setup

If using Gmail as the sender, you need to:

1. Enable 2-Step Verification on your Google account
2. Generate an [App Password](https://myaccount.google.com/apppasswords)
3. Use the App Password as `SENDER_PASSWORD`

## Usage

### Build and Run

```bash
go build -o ticket-checker
./ticket-checker
```

### Run Directly

```bash
go run main.go
```

## How It Works

1. The program loads configuration from environment variables (or `.env` file)
2. Uses headless Chrome to navigate to the target URL
3. Waits for the `.nextBtn` element to appear on the page
4. If the button is found, sends an email notification and exits
5. If not found, waits for the configured interval and checks again

## Customization

To monitor a different button or element, modify the selector in `main.go`:

```go
chromedp.WaitVisible(`.nextBtn`, chromedp.ByQuery),
```

Replace `.nextBtn` with your target CSS selector.

## License

MIT
