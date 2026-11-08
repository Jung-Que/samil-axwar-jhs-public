FROM node:22-alpine

WORKDIR /app
ENV NODE_ENV=production

COPY package.json server.js ./
COPY public ./public

EXPOSE 8080
USER node
CMD ["node", "server.js"]
