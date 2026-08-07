/**
 * @type {{ apps: import("pm2").StartOptions[] }}
 */
module.exports = {
  apps: [
    {
      name: 'alemonb',
      script: './index.js',
      env: {
        NODE_ENV: 'production'
      }
    }
  ]
};
