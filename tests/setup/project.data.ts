export const TEST_COMPOSE_YAML = `configs:
  some_content:
    content: |
      This is a test config file.
      It can contain multiple lines of text.
      Used for testing purposes.

services:
  nginx:
    image: public.ecr.aws/nginx/nginx:stable-alpine
    container_name: \${CONTAINER_NAME}
    configs:
      - source: some_content
        target: /config/test-content.txt
    volumes:
      - nginx_data:/config

volumes:
  nginx_data:
    driver: local
`;

export const TEST_ENV_FILE = `CONTAINER_NAME=test-nginx-container
`;
