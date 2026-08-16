# Example: Node.js with npm packages
# doneram: node:22.#.#
FROM node:22.0.0

# doneram: express:#.#.#, lodash:#.#.#
RUN npm install -g \
    express@4.18.2 \
    lodash@4.17.21

HEALTHCHECK CMD node -e "require('express'); require('lodash'); console.log('healthy')"
