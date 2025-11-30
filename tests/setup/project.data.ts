export const TEST_COMPOSE_YAML = `configs:
  some_content:
    content: |
      This is a test config file.
      It can contain multiple lines of text.
      Used for testing purposes.

services:
  redis:
    image: redis:latest
    container_name: \${CONTAINER_NAME}
    configs:
      - source: some_content
        target: /etc/some_content.txt
    command: /bin/sh -c 'cat /etc/some_content.txt && redis-server'
    ports:
      - "8081:81"
      - "6379:6379"
      - "6378:6378"
    volumes:
      - redis_data:/data

volumes:
  redis_data:
    driver: local
`;

export const TEST_ENV_FILE = `CONTAINER_NAME=test-redis-container
`;
