UniRouter Design Specification v1
Version: 1.0


Status: Implementable


Audience:


     • Founder
     • Product
     • Design
     • Frontend




1. Purpose
This document defines the operational UI specification for UniRouter.


The goal is not visual beautification.


The goal is to transform UniRouter from a CRUD admin panel into an Enterprise AI Activity Control Plane.




2. Design Objectives
Priority order:


P1 Visibility


P2 Control


P3 Intelligence


P4 Branding



Every screen must help users answer:


     1. What is happening?
     2. Is it healthy?
     3. What needs attention?
     4. What action should I take?




                                                   1
3. Navigation Architecture
Current
14 flat menu items


High cognitive load.


No hierarchy.


No grouping.




New Navigation
Overview


Traffic


Billing


Access


System




Overview

Dashboard




Traffic

Channels


Upstream Keys


API Keys


Logs


Channel Test




Billing

Wallet




                        2
Model Pricing


Organization Multipliers


Token Reconciliation


Grants




Access

Users


Audit Logs




System

Settings




4. Layout System
Global Layout
Sidebar Width


240px


Collapsed Width


72px



Header Height


56px



Content Max Width


None


Use full-width operational layout.



Page Padding




                                     3
24px


Desktop


16px


Laptop




5. Page Template System
All pages must use one of three templates.


No exceptions.



Template A


Resource Management


Examples:


Channels


Keys


Users


Pricing


Settings



Structure


Page Header


Filter Bar


Data View


Action Drawer



Template B


Explorer




                                             4
Examples:


Logs


Audit Logs


Reconciliation



Structure


Search Panel


Filters


Result Table


Detail Panel



Template C


Analytics


Examples:


Dashboard


Wallet



Structure


KPI Row


Trend Area


Distribution Area


Event Feed




6. Dashboard v2
Goal
Show enterprise AI activity.




                               5
Not configuration.



Section 1


KPI Cards


Total Requests


Success Rate


Average Latency


Today's Cost



Height


120px


Cards


4



Section 2


Activity Trend


Use existing daily_trend API.


24h trend chart.


Full width.


Height


320px



Section 3


Top Models


Ranked by request volume.


Table or horizontal bars.




                                6
Top 10.



Section 4


Channel Health


Channel


Status


Fail Count


Circuit State



Section 5


Recent Events


Timeline


Not table.



Remove


Model Pricing Table


from Dashboard.


Move to Billing.




7. Channels v2
Current issue:


Looks like a database table.



Goal:


Channels should feel like infrastructure assets.



View Mode




                                                   7
Card Grid


Default



Card Content


Provider Logo


Channel Name


Health Status


Circuit State


Fail Count


Models Count


Last Activity



Secondary View


Table


Optional toggle.



Card Actions


Edit


Disable


Delete


Test



Status Colors


Healthy


Green


Cooldown




                   8
Orange


Open Circuit


Red




8. API Keys v2
Keep table.



Add


Usage


Last Used


Status


Allowed Models Count



Quick Actions


Copy


Disable


Rotate




9. Logs Explorer v2
Highest priority page.



Layout


Left


Filters


Center


Results




                         9
Right


Detail Drawer



Filters


Model


Provider


Status Code


Time Range


User


Request ID



Result Table


Request ID


Model


Provider


Latency


Tokens


Cost


Status


Time



Detail Drawer


Metadata


Headers


Cost Breakdown


Latency Breakdown




                    10
Raw Response



Future


Trace Integration


No redesign required.




10. Wallet v2
Goal


Financial observability.



KPI


Available Balance


Frozen Balance


Granted Balance


Total Spend



Trend


Daily Spend



Distribution


Cost by Model



Records


Transaction History




11. Audit Logs
Keep table.




                           11
Add


Severity Tag


Info


Warning


Critical



Action Colors


Create


Green


Update


Blue


Delete


Red


Login


Purple




12. Design Tokens
Radius
Small


8px


Medium


12px


Large


16px




                    12
Default


12px




Spacing
4


8


12


16


24


32


48


64




Shadows
Level 0


None


Level 1


Soft


Level 2


Elevated



No heavy shadows.




13. Typography
Font


Inter




                    13
Fallback


System Sans



Page Title


28px


600



Section Title


18px


600



Body


14px


400



Caption


12px


400



Monospace


Request IDs


Token Values


Latency




14. Theme Engine
Architecture


Theme Provider




                   14
CSS Variables



Token Categories


Color


Radius


Shadow


Typography



Official Themes


UniRouter


GitHub


Cloudflare


Vercel



Theme switching


No reload required.




15. Color System
Primary


Brand



Success


Healthy



Warning


Attention




                      15
Danger


Failure



Info


Neutral



Color must indicate state.


Never decoration.




16. Dark Mode
Mandatory.



Dark mode is not inverted colors.



Use semantic tokens.


Never hardcode hex colors in components.




17. Icon System
Use Lucide.


Reduce Semi Icons.



Rules


Navigation icons


Outlined


Operational states


Filled




                                           16
No duplicate icon semantics.




18. Motion System
Duration


150ms


200ms


250ms



Use only for:


Hover


Expand


Drawer


Toast



No decorative animation.




19. Responsive Strategy
Desktop First.



Support


1440+


1280


1024



Mobile


Read-only dashboards only.




                               17
No mobile administration workflow.




20. Anti-Patterns
Do not add pages for future functionality.


Do not add fake metrics.


Do not add decorative charts.


Do not add glassmorphism.


Do not add AI-themed neon effects.


Do not add excessive gradients.


Do not copy OneAPI layouts.




21. Success Metrics
After redesign:


Users should identify system health within 10 seconds.


Users should locate a failed request within 30 seconds.


Users should understand AI usage trends within 60 seconds.


Navigation depth should remain ≤ 2 levels.




Final Principle
Every screen should make enterprise AI activity more visible, more controllable, and easier to
understand.




                                                   18
