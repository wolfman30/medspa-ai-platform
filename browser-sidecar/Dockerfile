# syntax=docker/dockerfile:1.7

##
## Build stage
##
FROM node:20-bookworm-slim AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY src ./src

# Build TypeScript
RUN npm run build

##
## Production stage
##
FROM mcr.microsoft.com/playwright:v1.41.0-jammy

WORKDIR /app

# Copy package files and install production dependencies only
# Skip browser download - we use the pre-installed browsers in the base image
COPY package*.json ./
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm ci --only=production && npm cache clean --force

# Copy built files from builder
COPY --from=builder /app/dist ./dist

# Ensure the app directory is owned by pwuser (the Playwright image's built-in non-root user)
RUN chown -R pwuser:pwuser /app

# Set environment variables
# PLAYWRIGHT_BROWSERS_PATH tells Playwright where the pre-installed browsers are
ENV NODE_ENV=production \
    PORT=3000 \
    HEADLESS=true \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:3000/health || exit 1

# Use the Playwright image's built-in non-root user (has access to /ms-playwright browsers)
USER pwuser

# Start the service
CMD ["node", "dist/index.js"]
