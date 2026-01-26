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

# Create non-root user
RUN groupadd -r browser && useradd -r -g browser browser

# Copy package files and install production dependencies only
COPY package*.json ./
RUN npm ci --only=production && npm cache clean --force

# Copy built files from builder
COPY --from=builder /app/dist ./dist

# Set environment variables
ENV NODE_ENV=production \
    PORT=3000 \
    HEADLESS=true

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:3000/health || exit 1

# Switch to non-root user
USER browser

# Start the service
CMD ["node", "dist/index.js"]
