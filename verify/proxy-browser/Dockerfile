FROM mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f

# Import test roots only inside this disposable browser image/container.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libnss3-tools \
    && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /proxy-browser-entrypoint.sh
ENTRYPOINT ["sh", "/proxy-browser-entrypoint.sh"]
