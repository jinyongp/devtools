FROM mcr.microsoft.com/playwright:v1.63.0-noble@sha256:eff16c30e6f3f4af0a03fa4b706120d5e9b0891c344a27d64559aff5900a4a27

# Import test roots only inside this disposable browser image/container.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libnss3-tools \
    && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /proxy-browser-entrypoint.sh
ENTRYPOINT ["sh", "/proxy-browser-entrypoint.sh"]
