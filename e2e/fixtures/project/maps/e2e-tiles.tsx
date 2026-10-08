<?xml version="1.0" encoding="UTF-8"?>
<!--
 Tiny 16: Basic, by Lanea Zimmerman (Sharm), CC-BY 4.0. See CREDITS.md.

 A real sheet rather than a generated pair of squares: 8 columns of 15 rows at
 16px, so the browser suite exercises the row-and-column arithmetic in
 Tileset.SourceRect that a two-tile placeholder never reaches.

  Tile art has no movement rule. A map object with Passability/Visibility
  components supplies that rule independently of this image.
-->
<tileset version="1.10" tiledversion="1.11.0" name="tiny16-basic" tilewidth="16" tileheight="16" tilecount="120" columns="8">
 <image source="tiny16-basic.png" width="128" height="240"/>
  <properties><property name="entityType" value="Floor"/><property name="Passability.kind" value="open"/><property name="Visibility.kind" value="clear"/></properties>
  <tile id="0" type="wall"><properties><property name="entityType" value="Wall"/><property name="Passability.kind" value="solid"/><property name="Visibility.kind" value="opaque"/></properties></tile>
  <tile id="11" type="grass"/>
  <tile id="13" type="water"><properties><property name="entityType" value="River"/><property name="Passability.kind" value="liquid"/><property name="Visibility.kind" value="clear"/></properties></tile>
  <tile id="14" type="path"/>
</tileset>
