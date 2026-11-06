import { createApp } from 'vue'
import App from './App.vue'
import { ui, location } from '@shopware-ag/meteor-admin-sdk'
import { initializeShopwareContext } from './services/shopware'

import './assets/main.css'

// Check if we're in the hidden main iframe or the actual content iframe
if (location.is(location.MAIN_HIDDEN)) {
  // This is the hidden iframe that registers the sidebar entry
  // It runs when Shopware loads the admin
  console.log('Registering sidebar entry...')
  
  ui.sidebar.add({
    title: 'Extension Demo',
    locationId: 'admin-extension-demo',
    icon: 'solid-puzzle-piece',
    resizable: true,
  }).then(() => {
    console.log('Sidebar entry registered successfully')
  }).catch((error) => {
    console.error('Failed to register sidebar entry:', error)
  })
} else {
  // This is the actual content iframe shown when user clicks the sidebar entry
  console.log('Initializing admin extension content...')
  
  initializeShopwareContext()
    .then(() => {
      const app = createApp(App)
      app.mount('#app')
      console.log('Admin extension mounted successfully')
    })
    .catch((error) => {
      console.error('Failed to initialize Shopware context:', error)
      // Show error in UI
      document.body.innerHTML = `
        <div style="padding: 20px; color: #e53e3e; background: #fff5f5; border: 1px solid #fc8181; border-radius: 4px; margin: 20px;">
          <h2>Failed to initialize</h2>
          <p>${error.message}</p>
          <p style="margin-top: 10px; font-size: 0.9em; color: #666;">
            Make sure the backend is running at: http://host.docker.internal:8085
          </p>
        </div>
      `
    })
}
