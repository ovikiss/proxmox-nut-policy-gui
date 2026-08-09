FROM python:3.12-slim
WORKDIR /app
ARG UI_SHARED_REPO=https://github.com/ovikiss/mikrotik-ui-shared.git
ARG UI_SHARED_REF=main
COPY requirements.txt .
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir -r requirements.txt
COPY app.py .
COPY templates templates
COPY static/header-controls.json static/header-controls.json
COPY scripts/sync-ui-shared.sh scripts/sync-ui-shared.sh
RUN UI_SHARED_REPO="$UI_SHARED_REPO" UI_SHARED_REF="$UI_SHARED_REF" sh scripts/sync-ui-shared.sh
VOLUME ["/data", "/root/.ssh"]
EXPOSE 8080
CMD ["gunicorn", "--bind", "0.0.0.0:8080", "--workers", "2", "app:app"]
