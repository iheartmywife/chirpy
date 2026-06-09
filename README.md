# Welcome to Chirpy!

Chirpy is a restful API server designed to handle backend operations for a social media platform. Built with Boot.Dev's Learn HTTP Servers in Go course, It's like twitter, but half as toxic and not nearly as useful.

With Chirpy, users can: 
- **Create and Manage "Chirps"**: Users can post short text updates.
- **Content Validation**: The server automatically scrubs "profane" words from posts to keep the platform friendly.
- **User Authentication**: Secure signup and login flows using JWTs (JSON Web Tokens) and Refresh Tokens.
- **Membership Tiers**: Integration with webhooks (like Stripe) to upgrade users to "Chirpy Red" status.

## Quick Example:
Scenario: A user wants to share their thoughts.

The client sends a POST request to /api/chirps with a JSON body:
```
{
  "body": "I love learning Go at Boot.dev!"
}
```
The server validates the token, cleans the text, and returns a 201 Created status with the saved object.

## INSTALLATION:

### Prerequisites: 
Go (version 1.26+)
HTTP request testing tool (e.g. Postman or curl)

### Installation
```
git clone https://github.com/iheartmywife/chirpy.git
cd chirpy
go mod download
```
### Running The Project
```
go build -o out && ./out
```

### Example Usage
```
curl http://localhost:8080/api/healthz
```