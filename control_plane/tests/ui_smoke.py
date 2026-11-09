"""Offline UI smoke test. Browser simulation does NOT validate real SSO, RBAC or proxy security."""
from pathlib import Path
from playwright.sync_api import sync_playwright

BASE = Path(__file__).resolve().parents[1]
HTML = BASE / 'AI_Workspace_Control_Plane_Prototype.html'
PREV = BASE / 'previews'
PREV.mkdir(exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True, executable_path='/usr/bin/chromium', args=['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu'])
    page = browser.new_page(viewport={'width': 1600, 'height': 980}, device_scale_factor=1)
    errors = []
    page.on('pageerror', lambda e: errors.append(str(e)))
    page.set_content(HTML.read_text(), wait_until='domcontentloaded')
    print('title:', page.title())
    assert page.locator('#page-home').is_visible()
    assert page.locator('#page-home .appcard').count() == 3, 'Admin should see three published apps'
    page.screenshot(path=str(PREV/'Control_Plane_Overview.png'), full_page=True)
    page.locator('button[data-page="catalog"]').click()
    assert page.locator('#page-catalog .appcard').count() == 3
    page.screenshot(path=str(PREV/'Control_Plane_App_Catalog.png'), full_page=True)
    page.locator('button[data-page="people"]').click()
    assert page.locator('#page-people tbody tr').count() == 4
    page.locator('#page-people button[data-action="editRoles:u-maya"]').click()
    assert page.locator('#modalBack').evaluate('(e)=>e.classList.contains("open")')
    page.locator('#cancelModal').click()
    page.locator('button[data-page="access"]').click()
    assert 'DENY' in page.locator('#page-access .decisionbox').inner_text(), 'Maya cannot access Agent Studio'
    page.locator('#accessApp').select_option('kms')
    assert 'ALLOW' in page.locator('#page-access .decisionbox').inner_text()
    page.screenshot(path=str(PREV/'Control_Plane_Access_Control.png'), full_page=True)
    page.locator('button[data-page="registry"]').click()
    assert page.locator('#page-registry tbody tr').count() == 4
    page.locator('button[data-action="newApp"]').click()
    page.locator('#f-key').fill('demo-reports')
    page.locator('#f-name').fill('Demo Reports')
    page.locator('#f-owner').fill('Platform Analytics')
    page.locator('#f-required').fill('kms:open')
    page.locator('#modalForm button[type=submit]').click()
    assert page.locator('#page-registry tbody tr').count() == 5
    assert page.locator('#page-registry tbody').inner_text().count('draft') >= 1
    page.locator('button[data-action="manageApp:demo-reports"]').click()
    page.locator('#modalBody select').select_option('published')
    page.locator('#modalForm button[type=submit]').click()
    page.locator('button[data-page="catalog"]').click()
    assert page.locator('#page-catalog .appcard').count() == 4
    page.locator('#page-catalog button[data-action="launch:demo-reports"]').click()
    assert 'No real mini-app' in page.locator('#modalSub').inner_text()
    page.locator('#cancelModal').click()
    page.locator('button[data-page="registry"]').click()
    page.locator('button[data-action="manageApp:demo-reports"]').click()
    page.locator('#modalBody select').select_option('disabled')
    page.locator('#modalForm button[type=submit]').click()
    page.locator('button[data-page="catalog"]').click()
    assert page.locator('#page-catalog .appcard').count() == 3
    page.locator('#actorSelect').select_option('u-maya')
    assert page.locator('#page-home .appcard').count() == 2, 'Maya should see KMS and Flowcase'
    page.locator('button[data-page="people"]').click()
    assert 'Access denied' in page.locator('#page-people').inner_text()
    page.locator('button[data-page="catalog"]').click()
    page.locator('button[data-action="catalogFilter:all"]').click()
    assert page.locator('#page-catalog .appcard').count() == 5
    assert page.locator('#page-catalog button[data-action="launch:agent-studio"]').is_disabled()
    page.locator('#workspaceSelect').select_option('legal-ops')
    assert page.locator('#page-home .appcard').count() == 1, 'Maya in Legal should see only KMS'
    page.locator('#workspaceSelect').select_option('sim-b2b')
    assert page.locator('#page-home .appcard').count() == 2
    page.locator('#actorSelect').select_option('u-rafi')
    page.locator('button[data-page="audit"]').click()
    assert 'Mini-app lifecycle updated' in page.locator('#page-audit').inner_text()
    assert not errors, errors
    print('desktop security simulation: PASS, JS errors:', errors)

    mobile = browser.new_page(viewport={'width':390,'height':844},device_scale_factor=1)
    merrors = []
    mobile.on('pageerror', lambda e:merrors.append(str(e)))
    mobile.set_content(HTML.read_text(),wait_until='domcontentloaded')
    mobile.screenshot(path=str(PREV/'Control_Plane_Mobile.png'), full_page=True)
    mobile.locator('#menuBtn').click()
    assert mobile.locator('#sidebar').evaluate('(e)=>e.classList.contains("open")')
    mobile.locator('button[data-page="catalog"]').click()
    assert mobile.locator('#page-catalog').is_visible()
    assert not merrors, merrors
    print('mobile navigation: PASS, JS errors:',merrors)
    browser.close()
