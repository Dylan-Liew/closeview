FROM alpine:3.22
COPY closeview /usr/local/bin/closeview
ENV HOME=/tmp
USER 1000:1000
ENTRYPOINT ["closeview"]
CMD ["serve", "--host", "127.0.0.1", "--port", "3434"]
