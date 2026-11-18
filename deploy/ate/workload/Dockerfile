FROM node:20-slim

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates curl python3 \
  && rm -rf /var/lib/apt/lists/* \
  && npm install -g @anthropic-ai/claude-code

WORKDIR /opt/shuttle-actor
COPY server.py /opt/shuttle-actor/server.py
RUN chmod +x /opt/shuttle-actor/server.py

ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["python3", "/opt/shuttle-actor/server.py"]
